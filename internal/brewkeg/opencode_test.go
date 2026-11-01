package brewkeg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A real config as OpenCode writes it: a schema line, an unrelated provider with
// its own settings, MCP servers, plugins and a default agent. Anything our merge
// gets wrong shows up here rather than in the app.
const opencodeFixture = `{
  "$schema": "https://opencode.ai/config.json",
  "default_agent": "build",
  "plugin": ["opencode-bash-guard-plugin"],
  "mcp": { "context7": { "type": "remote", "url": "https://mcp.context7.com/mcp" } },
  "providers": {
    "ollama": {
      "name": "Ollama",
      "package": "@ai-sdk/openai-compatible",
      "settings": { "baseURL": "http://localhost:11434/v1" },
      "models": { "gemma4:e4b-mlx": {} }
    }
  }
}`

func opencodePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.Getenv("HOME"), ".config", "opencode", "opencode.json")
}

func writeOpenCodeFixture(t *testing.T, body string) string {
	t.Helper()
	p := opencodePath(t)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func readOpenCode(t *testing.T, p string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s is not valid JSON after our write: %v", p, err)
	}
	return doc
}

// A user's providers are theirs. We add ours and touch nothing else.
func TestOpenCodeAddsProviderAndKeepsTheRest(t *testing.T) {
	home(t)
	p := writeOpenCodeFixture(t, opencodeFixture)

	o := Options{BaseURL: "https://brewkeg.dev", APIKey: "bk_live_TESTKEY"}.WithDefaults()
	if err := WriteOpenCodeProvider(p, o, []string{"brewkeg-astra", "gpt-6-astra"}); err != nil {
		t.Fatal(err)
	}

	doc := readOpenCode(t, p)
	if doc["$schema"] != "https://opencode.ai/config.json" {
		t.Fatalf("$schema lost: %v", doc["$schema"])
	}
	if doc["default_agent"] != "build" {
		t.Fatalf("unrelated key dropped: default_agent=%v", doc["default_agent"])
	}
	if doc["mcp"] == nil || doc["plugin"] == nil {
		t.Fatalf("mcp/plugin dropped from the file")
	}

	providers, _ := doc["providers"].(map[string]any)
	if providers == nil {
		t.Fatal("providers map gone")
	}
	ollama, _ := providers["ollama"].(map[string]any)
	if ollama == nil {
		t.Fatal("the user's own provider was dropped")
	}
	settings, _ := ollama["settings"].(map[string]any)
	if settings["baseURL"] != "http://localhost:11434/v1" {
		t.Fatalf("another provider's settings were rewritten: %v", ollama)
	}

	ours, _ := providers[OpenCodeProviderID].(map[string]any)
	if ours == nil {
		t.Fatal("providers.brewkeg was not written")
	}
	if ours["package"] != OpenCodePackage {
		t.Fatalf("package = %v, want %s", ours["package"], OpenCodePackage)
	}
	oset, _ := ours["settings"].(map[string]any)
	if oset["baseURL"] != "https://brewkeg.dev/v1" {
		t.Fatalf("baseURL = %v — OpenCode appends /chat/completions to this", oset["baseURL"])
	}
	if oset["apiKey"] != "bk_live_TESTKEY" {
		t.Fatalf("apiKey = %v", oset["apiKey"])
	}
	models, _ := ours["models"].(map[string]any)
	if len(models) != 2 {
		t.Fatalf("models = %v, want the two the spec published", models)
	}
	// The default model is what makes OpenCode actually route through us.
	if doc["model"] != "brewkeg/brewkeg-astra" {
		t.Fatalf("model = %v, want brewkeg/brewkeg-astra", doc["model"])
	}
}

// Turning the tool off has to take us back out, and only us.
func TestOpenCodeRemoveLeavesUserConfigIntact(t *testing.T) {
	home(t)
	p := writeOpenCodeFixture(t, opencodeFixture)
	o := Options{BaseURL: "https://brewkeg.dev", APIKey: "bk_live_TESTKEY"}.WithDefaults()

	if err := WriteOpenCodeProvider(p, o, []string{"brewkeg-astra"}); err != nil {
		t.Fatal(err)
	}
	changed, err := RemoveOpenCodeProvider(p)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("remove reported no change after a write")
	}

	doc := readOpenCode(t, p)
	providers, _ := doc["providers"].(map[string]any)
	if _, ours := providers[OpenCodeProviderID]; ours {
		t.Fatal("providers.brewkeg survived the removal")
	}
	if _, theirs := providers["ollama"]; !theirs {
		t.Fatal("the user's own provider was removed with ours")
	}
	if _, ok := doc["model"]; ok {
		t.Fatalf("a default model that named us survived: %v", doc["model"])
	}
	if doc["default_agent"] != "build" || doc["mcp"] == nil {
		t.Fatal("unrelated keys were dropped by the removal")
	}

	// Nothing of ours left means the switch must read off, or eviction would
	// keep trying to remove a provider that is already gone.
	if OpenCodePointsAtBrewkeg(p) {
		t.Fatal("the file still reports brewkeg after removal")
	}
}

// A user's own default model is theirs: we neither overwrite it nor delete it.
func TestOpenCodeLeavesForeignDefaultModelAlone(t *testing.T) {
	home(t)
	p := writeOpenCodeFixture(t, strings.Replace(opencodeFixture,
		`"default_agent": "build",`, `"default_agent": "build", "model": "ollama/gemma4:e4b-mlx",`, 1))

	if err := WriteOpenCodeProvider(p, Options{BaseURL: "https://brewkeg.dev", APIKey: "k"}.WithDefaults(), []string{"brewkeg-astra"}); err != nil {
		t.Fatal(err)
	}
	doc := readOpenCode(t, p)
	if doc["model"] != "ollama/gemma4:e4b-mlx" {
		t.Fatalf("the user's default model was overwritten: %v", doc["model"])
	}
	if _, ours := doc["providers"].(map[string]any)[OpenCodeProviderID]; !ours {
		t.Fatal("the provider was not added")
	}

	if _, err := RemoveOpenCodeProvider(p); err != nil {
		t.Fatal(err)
	}
	doc = readOpenCode(t, p)
	if doc["model"] != "ollama/gemma4:e4b-mlx" {
		t.Fatalf("the user's own default model was deleted: %v", doc["model"])
	}
}

// The switch state is read from the tool's own config, never from a marker.
func TestOpenCodeEnabledFollowsTheProviderEntry(t *testing.T) {
	home(t)
	p := writeOpenCodeFixture(t, opencodeFixture)

	tgt, ok := TargetByID("opencode")
	if !ok {
		t.Fatal("the built-in spec has no opencode target")
	}
	if enabledFor(tgt, DefaultSpec()) {
		t.Fatal("opencode reads as configured before we wrote anything")
	}

	if err := WriteOpenCodeProvider(p, Options{BaseURL: "https://brewkeg.dev", APIKey: "k"}.WithDefaults(), []string{"brewkeg-astra"}); err != nil {
		t.Fatal(err)
	}
	if !enabledFor(tgt, DefaultSpec()) {
		t.Fatal("opencode reads as off after we configured it")
	}
}

// A file we cannot parse is a tool the user cannot start. Refuse, and leave it
// exactly as it was.
func TestOpenCodeRefusesBrokenJSON(t *testing.T) {
	home(t)
	p := writeOpenCodeFixture(t, `{ "providers": `)

	err := WriteOpenCodeProvider(p, Options{BaseURL: "https://brewkeg.dev", APIKey: "k"}.WithDefaults(), []string{"brewkeg-astra"})
	if err == nil {
		t.Fatal("wrote into a file that does not parse")
	}
	raw, _ := os.ReadFile(p)
	if string(raw) != `{ "providers": ` {
		t.Fatalf("the broken file was modified: %q", raw)
	}
}
