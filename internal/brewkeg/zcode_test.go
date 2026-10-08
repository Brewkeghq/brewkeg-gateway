package brewkeg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A real registry as ZCode writes it: a schemaVersion, a provider order, a
// provider rule array and a model rule array. Anything our merge gets wrong
// shows up here rather than in the app.
const zcodeFixture = `{
  "schemaVersion": 1,
  "config": {
    "providerOrder": ["zai-coding-plan"],
    "providerConfigRules": {
      "providerRules": [
        {
          "providerId": "zai-coding-plan",
          "providerName": "Z.ai - Coding Plan",
          "config": {
            "group": "standard-personal",
            "access": { "type": "api-key", "apiKey": "z-keepme" },
            "api": { "type": "anthropic-messages", "baseUrl": "https://api.z.ai/api/anthropic" },
            "personalModelIds": ["glm-5.3"],
            "modelOrder": ["glm-5.3"]
          }
        }
      ]
    },
    "modelConfigRules": {
      "providerModelRules": [
        {
          "modelId": "glm-5.3",
          "config": { "enabled": true, "properties": { "contextWindow": 200000 } },
          "providerId": "zai-coding-plan"
        }
      ],
      "manualProviderModelRules": []
    }
  }
}`

func zcodePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.Getenv("HOME"), ".zcode", "v2", "provider_config.json")
}

func writeZCodeFixture(t *testing.T, body string) string {
	t.Helper()
	p := zcodePath(t)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func readZCode(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(ReadFile(zcodePath(t))), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func zcodeRules(t *testing.T) (providers, models []any) {
	t.Helper()
	cfg, _ := readZCode(t)["config"].(map[string]any)
	p, _ := cfg["providerConfigRules"].(map[string]any)
	m, _ := cfg["modelConfigRules"].(map[string]any)
	return asSlice(p["providerRules"]), asSlice(m["providerModelRules"])
}

func opts() Options {
	return Options{
		APIKey:      "bk_live_test",
		BaseURL:     "https://brewkeg.dev",
		MainModel:   "claude-opus-5",
		SonnetModel: "claude-sonnet-5",
		FastModel:   "claude-haiku-4-5",
	}
}

func TestWriteZCodeAddsBrewkegWithoutTouchingOtherProviders(t *testing.T) {
	withTempHome(t)
	writeZCodeFixture(t, zcodeFixture)

	if err := WriteZCodeProvider(zcodePath(t), opts()); err != nil {
		t.Fatal(err)
	}

	providers, models := zcodeRules(t)
	if len(providers) != 2 {
		t.Fatalf("expected 2 provider rules, got %d", len(providers))
	}
	if len(models) != 4 {
		t.Fatalf("expected our 3 model rules plus theirs, got %d", len(models))
	}

	// The user's own provider must survive intact, key for key.
	raw := ReadFile(zcodePath(t))
	for _, want := range []string{"zai-coding-plan", "z-keepme", "https://api.z.ai/api/anthropic", "glm-5.3", "200000"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("the user's own provider lost %q:\n%s", want, raw)
		}
	}

	// Ours, with the key names the app actually reads.
	if !ZCodePointsAtBrewkeg(zcodePath(t)) {
		t.Fatal("brewkeg is not in the registry after a write")
	}
	if idx := findRule(providers, "providerId", ZCodeProviderID); idx < 0 {
		t.Fatal("no brewkeg provider rule")
	} else {
		rule := providers[idx].(map[string]any)
		cfg := rule["config"].(map[string]any)
		access := cfg["access"].(map[string]any)
		api := cfg["api"].(map[string]any)
		if access["apiKey"] != "bk_live_test" || access["type"] != "api-key" {
			t.Fatalf("access block wrong: %+v", access)
		}
		// ZCode appends /messages, so /v1 has to be on the base or the
		// request lands on https://brewkeg.dev/messages and 404s.
		if api["baseUrl"] != "https://brewkeg.dev/v1" {
			t.Fatalf("baseUrl = %v, want https://brewkeg.dev/v1", api["baseUrl"])
		}
		if api["type"] != "anthropic-messages" {
			t.Fatalf("api type = %v", api["type"])
		}
		if got := asSlice(cfg["personalModelIds"]); len(got) != 3 {
			t.Fatalf("personalModelIds = %v", got)
		}
	}
}

// Running setup twice must not duplicate the provider or the model rules. The
// second run is the one a user does without thinking.
func TestWriteZCodeIsIdempotent(t *testing.T) {
	withTempHome(t)
	writeZCodeFixture(t, zcodeFixture)

	if err := WriteZCodeProvider(zcodePath(t), opts()); err != nil {
		t.Fatal(err)
	}
	first := ReadFile(zcodePath(t))
	if err := WriteZCodeProvider(zcodePath(t), opts()); err != nil {
		t.Fatal(err)
	}
	if second := ReadFile(zcodePath(t)); second != first {
		t.Fatalf("a second run changed the file:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// Re-running with a different key must update ours, not skip because a rule
// with our id already exists. This is the entire reason for edit-in-place.
func TestWriteZCodeUpdatesTheKeyInPlace(t *testing.T) {
	withTempHome(t)
	writeZCodeFixture(t, zcodeFixture)

	o := opts()
	if err := WriteZCodeProvider(zcodePath(t), o); err != nil {
		t.Fatal(err)
	}
	o.APIKey = "bk_live_rotated"
	if err := WriteZCodeProvider(zcodePath(t), o); err != nil {
		t.Fatal(err)
	}

	raw := ReadFile(zcodePath(t))
	if strings.Contains(raw, "bk_live_test\"") {
		t.Fatalf("the old key survived a rotation:\n%s", raw)
	}
	if !strings.Contains(raw, "bk_live_rotated") {
		t.Fatalf("the new key is missing:\n%s", raw)
	}
	providers, _ := zcodeRules(t)
	if len(providers) != 2 {
		t.Fatalf("rotation added a provider: %d rules", len(providers))
	}
}

// Fields on our own rule that we do not manage must survive. Someone who has
// hand-tuned a provider should not lose that by running setup.
func TestWriteZCodePreservesOurOwnUnknownFields(t *testing.T) {
	withTempHome(t)

	withOurs := `{"schemaVersion":1,"config":{
      "providerOrder":["brewkeg"],
      "providerConfigRules":{"providerRules":[{
        "providerId":"brewkeg","providerName":"brewkeg",
        "config":{"group":"standard-personal",
          "access":{"type":"api-key","apiKey":"old","extraSetting":"keep-me"},
          "api":{"type":"anthropic-messages","baseUrl":"https://old.example",
                 "timeoutSeconds":42},
          "personalModelIds":["claude-opus-5"],"modelOrder":["claude-opus-5"]}}]},
      "modelConfigRules":{"providerModelRules":[],"manualProviderModelRules":[]}}}`
	writeZCodeFixture(t, withOurs)

	if err := WriteZCodeProvider(zcodePath(t), opts()); err != nil {
		t.Fatal(err)
	}
	raw := ReadFile(zcodePath(t))
	for _, want := range []string{"extraSetting", "keep-me", "timeoutSeconds", "42"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("lost an unmanaged field %q:\n%s", want, raw)
		}
	}
	if strings.Contains(raw, "old.example") {
		t.Fatalf("our own baseUrl was not updated:\n%s", raw)
	}
}

// Dropping a model from the lineup has to drop its rule too, or ZCode keeps
// offering a model the user just took off the switcher.
func TestWriteZCodeDropsRulesForModelsNoLongerOffered(t *testing.T) {
	withTempHome(t)
	writeZCodeFixture(t, zcodeFixture)

	if err := WriteZCodeProvider(zcodePath(t), opts()); err != nil {
		t.Fatal(err)
	}
	o := opts()
	o.FastModel = ""
	if err := WriteZCodeProvider(zcodePath(t), o); err != nil {
		t.Fatal(err)
	}

	_, models := zcodeRules(t)
	for _, r := range models {
		rm := r.(map[string]any)
		if id, _ := rm["modelId"].(string); id == "claude-haiku-4-5" {
			t.Fatalf("a dropped model kept its rule: %+v", rm)
		}
	}
	if !strings.Contains(ReadFile(zcodePath(t)), "glm-5.3") {
		t.Fatal("the user's own model rule was removed")
	}
}

// Turning ZCode off must remove brewkeg and leave every other provider exactly
// where it was.
func TestRemoveZCodeProviderLeavesEveryoneElseAlone(t *testing.T) {
	withTempHome(t)
	writeZCodeFixture(t, zcodeFixture)
	if err := WriteZCodeProvider(zcodePath(t), opts()); err != nil {
		t.Fatal(err)
	}

	changed, err := RemoveZCodeProvider(zcodePath(t))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("removing a configured provider reported no change")
	}
	if ZCodePointsAtBrewkeg(zcodePath(t)) {
		t.Fatal("brewkeg survived the removal")
	}

	providers, models := zcodeRules(t)
	if len(providers) != 1 {
		t.Fatalf("expected only the user's own provider to remain, got %d", len(providers))
	}
	if len(models) != 1 {
		t.Fatalf("expected only the user's own model rule to remain, got %d", len(models))
	}
	raw := ReadFile(zcodePath(t))
	for _, want := range []string{"zai-coding-plan", "z-keepme", "https://api.z.ai/api/anthropic", "glm-5.3", "200000"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("removal damaged another provider (%q):\n%s", want, raw)
		}
	}
	if strings.Contains(raw, ZCodeProviderID) {
		t.Fatalf("a brewkeg reference is left behind:\n%s", raw)
	}
}

// A file ZCode has never written must be created rather than treated as an
// error, and must come out valid.
func TestWriteZCodeCreatesTheFileWhenThereIsNone(t *testing.T) {
	withTempHome(t)
	if err := WriteZCodeProvider(zcodePath(t), opts()); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(ReadFile(zcodePath(t))), &m); err != nil {
		t.Fatalf("created file is not valid JSON: %v", err)
	}
	if m["schemaVersion"] != float64(1) {
		t.Fatalf("schemaVersion = %v, want 1", m["schemaVersion"])
	}
}

// A file the user has corrupted must stop us, not get overwritten. Their
// settings are in there; a parse failure means we do not know what we would be
// destroying.
func TestWriteZCodeRefusesToClobberInvalidJSON(t *testing.T) {
	withTempHome(t)
	writeZCodeFixture(t, `{"schemaVersion":1,"config":`)

	err := WriteZCodeProvider(zcodePath(t), opts())
	if err == nil {
		t.Fatal("invalid JSON must be reported, not silently replaced")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("unhelpful error: %v", err)
	}
	if got := ReadFile(zcodePath(t)); !strings.Contains(got, `"config":`) {
		t.Fatalf("the user's broken file was overwritten: %s", got)
	}
}

// The provider must be first so the model picker opens on it.
func TestWriteZCodePutsBrewkegFirstInTheOrder(t *testing.T) {
	withTempHome(t)
	writeZCodeFixture(t, zcodeFixture)
	if err := WriteZCodeProvider(zcodePath(t), opts()); err != nil {
		t.Fatal(err)
	}
	cfg, _ := readZCode(t)["config"].(map[string]any)
	order := asSlice(cfg["providerOrder"])
	if len(order) == 0 || order[0] != ZCodeProviderID {
		t.Fatalf("providerOrder = %v", order)
	}
	if order[1] != "zai-coding-plan" {
		t.Fatalf("the user's provider was dropped from the order: %v", order)
	}
}
