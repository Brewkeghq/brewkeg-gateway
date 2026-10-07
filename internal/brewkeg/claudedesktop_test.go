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
