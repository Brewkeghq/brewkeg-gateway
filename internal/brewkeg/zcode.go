package brewkeg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ZCode is Z.ai's coding agent. Its model providers live in one plain JSON file
// that the desktop app, `zcode --web` and the terminal all read, so — unlike
// Cursor or Antigravity — there is a file to own and one edit is enough.
//
// The shape below was read off a real install, not from the docs: the provider
// registry nests two arrays of rule objects, and a flat top-level merge cannot
// express either of them. That is why this has a dedicated editor alongside
// `toml-provider` rather than being another file kind.
//
// What we touch, and nothing else:
//   config.providerConfigRules.providerRules[*].config.{access,api,personalModelIds,modelOrder}
//   config.providerOrder
//   config.modelConfigRules.providerModelRules[*]
// Entries belonging to any other providerId are left exactly as they are.

// ZCodeProviderID is our own key into the registry. It has to be stable: it is
// what tells our rule apart from a provider the user added themselves.
const ZCodeProviderID = "brewkeg"

// ZCodeProviderConfigPath is where the provider registry lives.
//
// Note there is no cli/config.json on this install. ZCode's CLI resolves its
// model from the same v2 file the desktop app uses, so writing a second copy
// for the terminal is how two configs drift apart.
func ZCodeProviderConfigPath() string {
	if base := os.Getenv("ZCODE_DATA_BASE_DIR"); base != "" {
		// The app writes its data under <base>/.zcode when this is set.
		return filepath.Join(base, ".zcode", "v2", "provider_config.json")
	}
	return filepath.Join(Home(), ".zcode", "v2", "provider_config.json")
}

// zcodeDoc mirrors the file. Only the parts we own are typed; everything else
// is carried through untouched by the round trip through map[string]any.
type zcodeDoc struct {
	SchemaVersion int            `json:"schemaVersion"`
	Config        map[string]any `json:"config"`
}

func loadZCode(path string) (map[string]any, error) {
	doc := map[string]any{}
	raw := ReadFile(path)
	if strings.TrimSpace(raw) == "" {
		return doc, nil
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON — fix or move it, then re-run", path)
	}
	return doc, nil
}

// dig walks a dotted path through nested maps, creating nothing.
func dig(m map[string]any, path ...string) (any, bool) {
	var cur any = m
	for _, k := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = mm[k]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// ensureMap walks a dotted path, creating intermediate maps as needed.
func ensureMap(m map[string]any, path ...string) map[string]any {
	cur := m
	for _, k := range path {
		next, ok := cur[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[k] = next
		}
		cur = next
	}
	return cur
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// findRule returns the index of the element in rules whose key field equals
// want, or -1.
func findRule(rules []any, field, want string) int {
	for i, r := range rules {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if s, _ := m[field].(string); s == want {
			return i
		}
	}
	return -1
}

// WriteZCodeProvider points ZCode at brewkeg and makes it the first choice.
func WriteZCodeProvider(path string, o Options) error {
	doc, err := loadZCode(path)
	if err != nil {
		return err
	}
	if v, ok := doc["schemaVersion"]; !ok {
		// A file we are creating ourselves: match what the app writes.
		doc["schemaVersion"] = 1
	} else if n, ok := v.(float64); !ok || n == 0 {
		doc["schemaVersion"] = 1
	}

	config := ensureMap(doc, "config")

	/* ------------------------------------------------ the provider itself */

	pcfg := ensureMap(config, "providerConfigRules")
	providers := asSlice(pcfg["providerRules"])

	rule := map[string]any{
		"providerId":   ZCodeProviderID,
		"providerName": "brewkeg",
		"config": map[string]any{
			"group": "standard-personal",
			"access": map[string]any{
				"type":   "api-key",
				"apiKey": o.APIKey,
			},
			"api": map[string]any{
				// ZCode appends /messages to this, so the /v1 has to be here
				// for the request to land on our /v1/messages.
				"type":    "anthropic-messages",
				"baseUrl": strings.TrimRight(o.BaseURL, "/") + "/v1",
			},
			"personalModelIds": toAnySlice(zcodeLineup(o)),
			"modelOrder":       toAnySlice(zcodeLineup(o)),
		},
	}

	if i := findRule(providers, "providerId", ZCodeProviderID); i >= 0 {
		// Edit in place so an id, a name or a setting we do not know about
		// survives. A wholesale replacement is how a user's own provider
		// tweaks get silently dropped.
		if existing, ok := providers[i].(map[string]any); ok {
			mergeInPlace(existing, rule)
		} else {
			providers[i] = rule
		}
	} else {
		providers = append([]any{rule}, providers...)
	}
	pcfg["providerRules"] = providers

	/* ----------------------------------------------------- providerOrder */

	order := []any{}
	for _, v := range asSlice(config["providerOrder"]) {
		if s, _ := v.(string); s != ZCodeProviderID {
			order = append(order, v)
		}
	}
	// First, so it is the one the model picker opens on.
	config["providerOrder"] = append([]any{ZCodeProviderID}, order...)

	/* ------------------------------------------------------ model rules */

	mcfg := ensureMap(config, "modelConfigRules")
	rules := []any{}
	kept := map[string]bool{}
	for _, m := range zcodeLineup(o) {
		kept[m] = true
	}

	for _, r := range asSlice(mcfg["providerModelRules"]) {
		rm, ok := r.(map[string]any)
		if !ok {
			rules = append(rules, r)
			continue
		}
		id, _ := rm["modelId"].(string)
		// Ours: keep if still on the lineup, drop if the user took it off.
		// Anyone else's: never ours to remove.
		if pid, _ := rm["providerId"].(string); pid == ZCodeProviderID {
			if !kept[id] {
				continue
			}
			if c, ok := rm["config"].(map[string]any); ok {
				c["enabled"] = true
			}
			rules = append(rules, rm)
			continue
		}
		rules = append(rules, r)
	}
	for _, m := range zcodeLineup(o) {
		if findRule(rules, "modelId", m) < 0 {
			rules = append(rules, map[string]any{
				"modelId": m,
				// No contextWindow: we do not publish one, and inventing a
				// number here would be a claim we cannot stand behind.
				"config":     map[string]any{"enabled": true},
				"providerId": ZCodeProviderID,
			})
		}
	}
	mcfg["providerModelRules"] = rules
	if _, ok := mcfg["manualProviderModelRules"]; !ok {
		mcfg["manualProviderModelRules"] = []any{}
	}

	return writeJSONDoc(path, doc)
}

// RemoveZCodeProvider takes brewkeg back out, leaving every other provider and
// every other model rule alone.
func RemoveZCodeProvider(path string) (bool, error) {
	doc, err := loadZCode(path)
	if err != nil {
		return false, err
	}
	config, _ := doc["config"].(map[string]any)
	if config == nil {
		return false, nil
	}

	changed := false

	if pcfg, ok := config["providerConfigRules"].(map[string]any); ok {
		if len(asSlice(pcfg["providerRules"])) > 0 {
			providers := []any{}
			for _, r := range asSlice(pcfg["providerRules"]) {
				rm, _ := r.(map[string]any)
				if pid, _ := rm["providerId"].(string); pid == ZCodeProviderID {
					changed = true
					continue
				}
				providers = append(providers, r)
			}
			if changed {
				pcfg["providerRules"] = providers
			}
		}
	}

	if len(asSlice(config["providerOrder"])) > 0 {
		order := []any{}
		for _, v := range asSlice(config["providerOrder"]) {
			if s, _ := v.(string); s == ZCodeProviderID {
				changed = true
				continue
			}
			order = append(order, v)
		}
		if changed {
			config["providerOrder"] = order
		}
	}

	if mcfg, ok := config["modelConfigRules"].(map[string]any); ok {
		if len(asSlice(mcfg["providerModelRules"])) > 0 {
			rules := []any{}
			for _, r := range asSlice(mcfg["providerModelRules"]) {
				rm, _ := r.(map[string]any)
				if pid, _ := rm["providerId"].(string); pid == ZCodeProviderID {
					changed = true
					continue
				}
				rules = append(rules, r)
			}
			if changed {
				mcfg["providerModelRules"] = rules
			}
		}
	}

	if !changed {
		return false, nil
	}
	return true, writeJSONDoc(path, doc)
}

// ZCodePointsAtBrewkeg reports whether our provider is in the registry. It
// looks for the provider, not for our marker: someone who wired brewkeg in by
// hand must see the same switch state as someone who used the app.
func ZCodePointsAtBrewkeg(path string) bool {
	doc, err := loadZCode(path)
	if err != nil {
		return false
	}
	config, _ := doc["config"].(map[string]any)
	pcfg, _ := config["providerConfigRules"].(map[string]any)
	if findRule(asSlice(pcfg["providerRules"]), "providerId", ZCodeProviderID) < 0 {
		return false
	}
	return true
}

/* ------------------------------------------------------------------ util */

// zcodeLineup is the same three-model rule the rest of the app follows: a
// partial lineup in Claude Desktop deletes a row from the picker, and the same
// instinct applies to a model list.
func zcodeLineup(o Options) []string {
	var out []string
	for _, m := range []string{o.MainModel, o.SonnetModel, o.FastModel} {
		if m != "" {
			out = append(out, m)
		}
	}
	return out
}

func toAnySlice(in []string) []any {
	out := make([]any, 0, len(in))
	for _, s := range in {
		out = append(out, s)
	}
	return out
}

// mergeInPlace copies the keys of src into dst, one level deep, recursing into
// nested objects so access/api keep any fields we do not manage.
func mergeInPlace(dst, src map[string]any) {
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			if existing, ok := dst[k].(map[string]any); ok {
				mergeInPlace(existing, sub)
				continue
			}
		}
		dst[k] = v
	}
}

func writeJSONDoc(path string, doc map[string]any) error {
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(path, string(out)+"\n")
}