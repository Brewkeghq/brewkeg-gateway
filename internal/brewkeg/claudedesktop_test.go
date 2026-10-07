package brewkeg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write3pProfile reproduces the on-disk layout Claude Desktop uses: a
// configLibrary directory with _meta.json naming the applied profile.
func write3pProfile(t *testing.T, appliedID, body string) string {
	t.Helper()
	dir := ClaudeDesktopConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if appliedID != "" {
		if err := os.WriteFile(filepath.Join(dir, appliedID+".json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		meta := `{"appliedId":"` + appliedID + `","entries":[{"id":"` + appliedID + `","name":"Default"}]}`
		if err := os.WriteFile(filepath.Join(dir, "_meta.json"), []byte(meta), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The path is not a constant: it is whatever _meta.json says is applied. That is
// the whole reason the engine resolves it instead of being handed a string.
func TestAppliedConfigFollowsMeta(t *testing.T) {
	withTempHome(t)

	dir := write3pProfile(t, "a664da21-038d-464d-8684-1b7110f4c9dd", `{"inferenceProvider":"anthropic"}`)
	want := filepath.Join(dir, "a664da21-038d-464d-8684-1b7110f4c9dd.json")
	if got := ClaudeDesktopAppliedConfig(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// Switching profiles must move with it, not stay pinned to the first.
	write3pProfile(t, "second-profile", `{"inferenceProvider":"gateway"}`)
	if got := ClaudeDesktopAppliedConfig(); got != filepath.Join(dir, "second-profile.json") {
		t.Fatalf("did not follow appliedId, got %q", got)
	}
}

// A machine that has never opened the third-party panel has no profile. We must
// write nothing rather than invent a _meta.json shape.
func TestNoProfileMeansNoWrite(t *testing.T) {
	withTempHome(t)
	if got := ClaudeDesktopAppliedConfig(); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
	if JSONHasAnyKey(ClaudeDesktopAppliedConfig(), []string{"inferenceGatewayBaseUrl"}) {
		t.Error("a missing config must not read as configured")
	}
}

// _meta.json is a file we do not control. An id containing a path separator or
// ".." must not let the write escape configLibrary/.
func TestAppliedIdCannotEscapeTheDirectory(t *testing.T) {
	withTempHome(t)
	dir := write3pProfile(t, "safe", `{}`)
	if err := os.WriteFile(filepath.Join(dir, "_meta.json"),
		[]byte(`{"appliedId":"../../../../tmp/pwned"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ClaudeDesktopAppliedConfig(); got != "" {
		t.Fatalf("a traversal id resolved to %q", got)
	}
	if FileExists("/tmp/pwned.json") {
		t.Fatal("traversal escaped the config directory")
	}
}

func TestMalformedMetaReadsAsNoProfile(t *testing.T) {
	withTempHome(t)
	dir := write3pProfile(t, "ok", `{}`)
	if err := os.WriteFile(filepath.Join(dir, "_meta.json"), []byte(`{"appliedId":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ClaudeDesktopAppliedConfig(); got != "" {
		t.Fatalf("got %q, want empty for malformed meta", got)
	}
}

// The applied profile holds the user's own app settings. We add our keys and
// leave every one of theirs alone.
func TestGatewayMergeKeepsTheUsersProfile(t *testing.T) {
	withTempHome(t)
	write3pProfile(t, "p1", `{
  "chatTabEnabled": true,
  "coworkEgressAllowedHosts": ["*"],
  "inferenceProvider": "anthropic",
  "inferenceCredentialKind": "interactive"
}`)
	path := ClaudeDesktopAppliedConfig()

	entries := []KVSpec{
		{Name: "inferenceProvider", Value: `"gateway"`},
		{Name: "inferenceGatewayBaseUrl", Value: `"https://brewkeg.dev"`},
		{Name: "inferenceCredentialKind", Value: `"static"`},
		{Name: "inferenceGatewayApiKey", Value: `"bk_live_x"`},
		{Name: "modelDiscoveryEnabled", Value: "false"},
		{Name: "inferenceModels", Value: `["claude-opus-5","claude-haiku-4-5"]`},
	}
	if err := writeJSONPlain(path, entries, Options{}); err != nil {
		t.Fatal(err)
	}

	doc := map[string]any{}
	if err := jsonUnmarshalForTest(ReadFile(path), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["chatTabEnabled"] != true {
		t.Errorf("chatTabEnabled lost: %#v", doc["chatTabEnabled"])
	}
	if _, ok := doc["coworkEgressAllowedHosts"].([]any); !ok {
		t.Errorf("host list lost: %#v", doc["coworkEgressAllowedHosts"])
	}
	// We take over the inference keys, including the credential kind, because
	// leaving "interactive" behind next to a static key is a broken pair.
	if doc["inferenceProvider"] != "gateway" || doc["inferenceCredentialKind"] != "static" {
		t.Errorf("inference keys not switched: %#v", doc)
	}
	// Types survive: a boolean false and a real array, not "false" and a string.
	if doc["modelDiscoveryEnabled"] != false {
		t.Errorf("modelDiscoveryEnabled = %#v, want boolean false", doc["modelDiscoveryEnabled"])
	}
	models, ok := doc["inferenceModels"].([]any)
	if !ok || len(models) != 2 || models[0] != "claude-opus-5" {
		t.Errorf("inferenceModels = %#v, want a 2-entry array", doc["inferenceModels"])
	}
	if !strings.Contains(ReadFile(path), `"inferenceGatewayApiKey"`) {
		t.Error("api key not written")
	}
}

func jsonUnmarshalForTest(raw string, v any) error { return json.Unmarshal([]byte(raw), v) }

// Claude Desktop's picker is not a free list. It has one fixed slot per family
// — sonnet, opus, haiku, fable, mythos — and shows a slot only when a model in
// inferenceModels resolves to it. So a lineup that simply omits sonnet does not
// error: the user's switcher quietly loses a row and they have no way to tell
// why. The lineup therefore has to be asserted as a whole.
func TestDesktopLineupCoversEveryFamilyThePickerOffers(t *testing.T) {
	withTempHome(t)
	write3pProfile(t, "p1", `{"inferenceProvider":"anthropic"}`)
	path := ClaudeDesktopAppliedConfig()

	o := Options{
		APIKey:      "bk_live_x",
		BaseURL:     "https://brewkeg.dev",
		MainModel:   "claude-opus-5",
		SonnetModel: "claude-sonnet-5",
		FastModel:   "claude-haiku-4-5",
	}

	var spec Spec
	for _, ts := range DefaultSpec().Targets {
		if ts.ID == "desktop" && ts.Files[0].Kind == "desktop-3p" {
			spec = Spec{Targets: []TargetSpec{ts}}
		}
	}
	if len(spec.Targets) != 1 {
		t.Fatal("no desktop-3p target in the built-in spec")
	}

	if err := writeJSONPlain(path, spec.Targets[0].Files[0].Entries, o); err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(ReadFile(path)), &doc); err != nil {
		t.Fatal(err)
	}
	models, ok := doc["inferenceModels"].([]any)
	if !ok {
		t.Fatalf("inferenceModels is not an array: %#v", doc["inferenceModels"])
	}

	families := map[string]bool{}
	for _, m := range models {
		id, _ := m.(string)
		for _, f := range []string{"opus", "sonnet", "haiku"} {
			if strings.Contains(id, f) {
				families[f] = true
			}
		}
	}
	for _, f := range []string{"opus", "sonnet", "haiku"} {
		if !families[f] {
			t.Errorf("no %s model in the lineup %v — its slot will be missing from the picker", f, models)
		}
	}
}

// An empty Options must still produce a complete lineup; the defaults are the
// engine's job, not the caller's.
func TestLineupDefaultsToAllThree(t *testing.T) {
	withTempHome(t)
	write3pProfile(t, "p1", `{}`)
	o := Options{}.WithDefaults()
	if o.MainModel == "" || o.SonnetModel == "" || o.FastModel == "" {
		t.Fatalf("a default lineup has a hole: %+v", o)
	}
	if o.MainModel == o.SonnetModel || o.SonnetModel == o.FastModel || o.MainModel == o.FastModel {
		t.Errorf("defaults collapsed: main=%q sonnet=%q fast=%q", o.MainModel, o.SonnetModel, o.FastModel)
	}
}
