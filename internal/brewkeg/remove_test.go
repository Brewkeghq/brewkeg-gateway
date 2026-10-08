package brewkeg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Turning a tool off has to undo turning it on. The strongest form of that claim
// is byte-equality: the file the user had before we ever touched it is the file
// they have after a connect/disconnect round trip. Anything less means eviction
// is leaving a fingerprint behind.
//
// The baseline is in json.MarshalIndent's canonical shape on purpose. Any JSON
// write we make — connect or disconnect — reformats the whole document, so
// "compact array stays compact" is not a property this engine has and the test
// should not imply it. What is being asserted is that the second write adds
// nothing further: one reformat, then stability.
func TestEvictRoundTripsClaudeCLIByteExact(t *testing.T) {
	withTempHome(t)

	orig := `{
  "env": {
    "FOO": "keep-me"
  },
  "permissions": {
    "allow": [
      "Bash(ls:*)"
    ]
  }
}
`
	path := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")
	writeFile(t, path, orig)

	if _, _, err := ApplyWithSpec(DefaultSpec(), []string{"claude-cli"}, Options{APIKey: "bk_live_K"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	on := readFile(t, path)
	for _, want := range []string{"ANTHROPIC_BASE_URL", "bk_live_K", "FOO", "permissions"} {
		if !strings.Contains(on, want) {
			t.Fatalf("after apply, %q missing:\n%s", want, on)
		}
	}

	_, results, err := ApplyWithSpec(DefaultSpec(), nil, Options{})
	if err != nil {
		t.Fatalf("evict: %v", err)
	}
	if len(results) != 1 || !results[0].Removed || !results[0].OK {
		t.Fatalf("evict should report one successful removal, got %+v", results)
	}

	if got := readFile(t, path); got != orig {
		t.Fatalf("round trip is not byte-exact\n--- got ---\n%s\n--- want ---\n%s", got, orig)
	}
}

// The codex round trip is the one with teeth: we comment out the user's own
// model_provider on the way in, so the way out has to put it back exactly.
func TestEvictRoundTripsCodexAndRestoresRootKey(t *testing.T) {
	withTempHome(t)

	orig := `model = "gpt-5.5"
model_provider = "openai"

[tui]
theme = "dark"
`
	path := filepath.Join(os.Getenv("HOME"), ".codex", "config.toml")
	writeFile(t, path, orig)

	if _, _, err := ApplyWithSpec(DefaultSpec(), []string{"codex"}, Options{APIKey: "bk_live_K"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	on := readFile(t, path)
	if !strings.Contains(on, "# brewkeg replaced: model_provider = \"openai\"") {
		t.Fatalf("apply did not comment out the user's own provider:\n%s", on)
	}
	if !TableHas(on, "model_providers.brewkeg") {
		t.Fatalf("apply did not add the provider table:\n%s", on)
	}
	if !strings.Contains(on, "theme = \"dark\"") {
		t.Fatalf("apply destroyed the user's own table:\n%s", on)
	}

	if _, _, err := ApplyWithSpec(DefaultSpec(), nil, Options{}); err != nil {
		t.Fatalf("evict: %v", err)
	}
	if got := readFile(t, path); got != orig {
		t.Fatalf("round trip is not byte-exact\n--- got ---\n%s\n--- want ---\n%s", got, orig)
	}
}

// An empty env object we created is our own residue. Leaving it behind means
// every future diff of the file shows a change the user never made.
func TestEvictDropsEmptiedEnvObject(t *testing.T) {
	withTempHome(t)

	path := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")
	writeFile(t, path, "{\n  \"permissions\": {\"allow\": [\"Bash\"]}\n}\n")

	if _, _, err := ApplyWithSpec(DefaultSpec(), []string{"claude-cli"}, Options{APIKey: "bk_live_K"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ApplyWithSpec(DefaultSpec(), nil, Options{}); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, path)
	if strings.Contains(got, `"env"`) {
		t.Fatalf("emptied env object left behind:\n%s", got)
	}
	if !strings.Contains(got, "permissions") {
		t.Fatalf("evict removed something that is not ours:\n%s", got)
	}
}

// Eviction must not reach into a tool the user never turned on. A codex-only
// setup has to leave ~/.claude completely alone.
func TestEvictLeavesNeverConfiguredTargetsAlone(t *testing.T) {
	withTempHome(t)

	path := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")
	writeFile(t, path, `{"hooks":{"a":1}}`+"\n")
	before := readFile(t, path)

	if _, _, err := ApplyWithSpec(DefaultSpec(), []string{"codex"}, Options{APIKey: "bk_live_K"}); err != nil {
		t.Fatal(err)
	}
	_, results, err := ApplyWithSpec(DefaultSpec(), []string{"codex"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.ID == "claude-cli" {
			t.Fatalf("evicted a target that was never configured: %+v", results)
		}
	}
	if got := readFile(t, path); got != before {
		t.Fatalf("claude-cli was touched by a codex-only run:\n%s", got)
	}
}

// A pure disconnect must not claim to have stored a key. Nothing was written
// anywhere, so remembering one would be a lie the next launch acts on.
func TestPureEvictionDoesNotStoreKey(t *testing.T) {
	withTempHome(t)

	if _, _, err := ApplyWithSpec(DefaultSpec(), []string{"claude-cli"}, Options{APIKey: "bk_live_FIRST"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ApplyWithSpec(DefaultSpec(), nil, Options{APIKey: "bk_live_SECOND"}); err != nil {
		t.Fatal(err)
	}
	if got := StoredKey(); got != "bk_live_FIRST" {
		t.Fatalf("disconnect changed the stored key to %q", got)
	}
}

// Restoring a root key into a file that already has a live assignment would be a
// duplicate-key parse error, and a config.toml that will not parse is a Codex
// the user cannot start. Refuse instead.
func TestRestoreRootKeyRefusesWhenAValueIsAlreadyLive(t *testing.T) {
	marked := `model = "gpt-5.5"
# brewkeg replaced: model_provider = "openai"
model_provider = "brewkeg"
`
	got, changed := restoreRootKey(marked, "model_provider")
	if changed {
		t.Fatalf("restored into a file that already has a live value:\n%s", got)
	}
	if !strings.Contains(got, "brewkeg") {
		t.Fatalf("the live value was destroyed:\n%s", got)
	}
}

// The same key inside a table belongs to that table, so it must not block the
// restore of the root-scope one.
func TestRestoreRootKeyIgnoresKeysInsideTables(t *testing.T) {
	marked := `# brewkeg replaced: model_provider = "openai"

[profiles.work]
model_provider = "azure"
`
	got, changed := restoreRootKey(marked, "model_provider")
	if !changed {
		t.Fatal("a table-scoped key wrongly blocked the restore")
	}
	if !strings.Contains(got, `model_provider = "openai"`) {
		t.Fatalf("value was not restored:\n%s", got)
	}
	if !strings.Contains(got, `model_provider = "azure"`) {
		t.Fatalf("table-scoped key was clobbered:\n%s", got)
	}
	if strings.Contains(got, replacedPrefix) {
		t.Fatalf("the comment was left behind as well as the live line:\n%s", got)
	}
}

// removeTable must take the header and everything under it, and stop at the
// next header. Taking one line too few leaves orphaned keys that Codex parses
// as belonging to the following table.
func TestRemoveTableStopsAtTheNextHeader(t *testing.T) {
	in := `[model_providers.brewkeg]
name = "brewkeg"
base_url = "https://brewkeg.dev/v1"

[model_providers.other]
name = "other"
`
	got, ok := removeTable(in, "model_providers.brewkeg")
	if !ok {
		t.Fatal("table not found")
	}
	if strings.Contains(got, "brewkeg") {
		t.Fatalf("table body survived:\n%s", got)
	}
	if !strings.Contains(got, `name = "other"`) {
		t.Fatalf("the next table was eaten too:\n%s", got)
	}
}

// A picker we installed is advertising models from a gateway that has just been
// switched off. Leaving it in place is the quiet failure mode: the tool looks
// configured and answers with a model the user just disconnected.
func TestEvictRemovesThePickerWeInstalled(t *testing.T) {
	withTempHome(t)

	path := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")
	writeFile(t, path, "{}\n")

	spec := DefaultSpec()
	spec.Pickers = map[string]ModelPickerSpec{
		"claude-cli": {Path: ".claude/settings.json", Replace: true, Options: []PickerOption{{Model: "claude-opus-5"}}},
	}
	if _, _, err := ApplyWithSpec(spec, []string{"claude-cli"}, Options{APIKey: "bk_live_K"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, path), "modelPicker") {
		t.Fatal("picker was not written")
	}
	if _, _, err := ApplyWithSpec(spec, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); strings.Contains(got, "modelPicker") || strings.Contains(got, "claude-opus-5") {
		t.Fatalf("picker survived a disconnect:\n%s", got)
	}
}

// AnyEnabled is what tells "disconnect everything" apart from a dead button.
func TestAnyEnabledTracksRealConfigState(t *testing.T) {
	withTempHome(t)

	if AnyEnabled() {
		t.Fatal("a machine with no brewkeg config reported as enabled")
	}
	if _, _, err := ApplyWithSpec(DefaultSpec(), []string{"claude-cli"}, Options{APIKey: "bk_live_K"}); err != nil {
		t.Fatal(err)
	}
	if !AnyEnabled() {
		t.Fatal("a configured machine reported as disabled")
	}
	if _, _, err := ApplyWithSpec(DefaultSpec(), nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if AnyEnabled() {
		t.Fatal("still enabled after disconnecting")
	}
}
