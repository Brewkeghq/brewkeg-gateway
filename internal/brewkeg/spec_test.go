package brewkeg

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchSpecReadsServerConfig(t *testing.T) {
	want := Spec{Version: 99, Targets: []TargetSpec{{ID: "codex", Label: "Codex CLI"}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/client-config" {
			t.Errorf("spec should be fetched from /api/client-config, got %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	got, err := FetchSpec(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 99 || got.Targets[0].ID != "codex" {
		t.Fatalf("server spec not used: %+v", got)
	}
}

// A dead or slow gateway must never stop someone configuring their tools.
func TestFetchSpecFallsBackOffline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	got, err := FetchSpec(context.Background(), srv.URL)
	if err == nil {
		t.Error("a 500 should be reported as an error")
	}
	if len(got.Targets) == 0 {
		t.Fatal("must fall back to the built-in spec")
	}
	if got.Version != DefaultSpec().Version {
		t.Errorf("fallback version = %d", got.Version)
	}
}

// The whole point of the spec: a server change must reach an installed app
// without a new release, and the change must be applied safely.
func TestServerSpecChangesCodexWithoutNewRelease(t *testing.T) {
	h := home(t)
	toml := "model = \"gpt-5\"\n\n[tui]\ntheme = \"dark\"\n"
	p := filepath.Join(h, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	// The server decides codex now needs a different wire_api and an extra key.
	server := DefaultSpec()
	for i := range server.Targets {
		if server.Targets[i].ID != "codex" {
			continue
		}
		server.Targets[i].Files[0].Entries = append(server.Targets[i].Files[0].Entries,
			KVSpec{Name: "requires_openai_auth", Value: "false"})
		for j, e := range server.Targets[i].Files[0].Entries {
			if e.Name == "wire_api" {
				server.Targets[i].Files[0].Entries[j] = KVSpec{Name: "wire_api", Value: `"chat"`}
			}
		}
	}
	server.Version = DefaultSpec().Version + 1

	if _, _, err := ApplyWithSpec(server, []string{"codex"}, Options{APIKey: "bk_live_K"}); err != nil {
		t.Fatal(err)
	}

	got := ReadFile(p)
	if !strings.Contains(got, `wire_api = "chat"`) {
		t.Errorf("server's new wire_api did not reach the file:\n%s", got)
	}
	if !strings.Contains(got, "requires_openai_auth = false") {
		t.Errorf("server's new key did not reach the file:\n%s", got)
	}
	if !strings.Contains(got, "[tui]") || !strings.Contains(got, `model = "gpt-5"`) {
		t.Errorf("the user's own config was not preserved:\n%s", got)
	}
}

func TestFetchSpecRefusesOlderThanBuiltIn(t *testing.T) {
	old := Spec{Version: 0, Targets: DefaultSpec().Targets}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(old)
	}))
	defer srv.Close()

	got, err := FetchSpec(context.Background(), srv.URL)
	if err == nil {
		t.Error("an older server spec must be refused, not silently downgrade the app")
	}
	if got.Version != DefaultSpec().Version {
		t.Errorf("should have kept the built-in spec, got v%d", got.Version)
	}
}

func TestSpecIsTheOnlyPlacePathsAndKeysLive(t *testing.T) {
	// A new tool must be addable by editing data, not Go. If this fails, the
	// spec is not actually driving the engine.
	spec := DefaultSpec()
	spec.Targets = append(spec.Targets, TargetSpec{
		ID: "opencode", Label: "OpenCode", Icon: "brewkeg",
		Detect: DetectSpec{Dirs: []string{".opencode"}},
		Enabled: EnabledSpec{
			JSONEnvKeys: []string{"OPENCODE_BASE_URL"},
		},
		Files: []FileSpec{{Path: ".opencode/config.json", Kind: "json-env",
			Entries: []KVSpec{{Name: "OPENCODE_BASE_URL", Value: `"{{baseUrlV1}}"`}}}},
	})

	tgts := TargetsFrom(spec)
	if len(tgts) != len(spec.Targets) {
		t.Fatalf("expected %d targets from spec, got %d", len(spec.Targets), len(tgts))
	}
	if _, ok := TargetByIDFrom(spec, "opencode"); !ok {
		t.Fatal("a tool that exists only in the spec must be selectable")
	}
}

// The picker block lives inside a settings file that also holds env, hooks and
// permissions. Replacing the picker must not disturb any of them.
func TestPickerReplacesOptionsAndKeepsEverythingElse(t *testing.T) {
	h := home(t)
	settings := filepath.Join(h, ".claude", "settings.json")
	before := `{
  "permissions": { "allow": ["Bash"] },
  "hooks": { "PreToolUse": [] },
  "modelPicker": {
    "replaceBuiltInOptions": false,
    "options": [{ "model": "corporate-sonnet-v1", "label": "Company Sonnet" }]
  },
  "env": { "KEEP": "me" }
}`
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := DefaultSpec()
	spec.Pickers = map[string]ModelPickerSpec{
		"claude-cli": {
			Path:    ".claude/settings.json",
			Replace: true,
			Options: []PickerOption{
				{Model: "claude-opus-5", Label: "Opus 5 · brewkeg", Description: "200K context"},
				{Model: "claude-haiku-4-5", Label: "Haiku 4.5 · brewkeg"},
			},
		},
	}

	b, _, err := ApplyWithSpec(spec, []string{"claude-cli"}, Options{APIKey: "bk_live_K"})
	if err != nil {
		t.Fatal(err)
	}

	got := ReadFile(settings)
	for _, keep := range []string{`"allow"`, `"PreToolUse"`, `"KEEP": "me"`, "ANTHROPIC_BASE_URL"} {
		if !strings.Contains(got, keep) {
			t.Errorf("picker write lost %q:\n%s", keep, got)
		}
	}
	if strings.Contains(got, "corporate-sonnet-v1") {
		t.Errorf("the old picker option should be gone when replace is true:\n%s", got)
	}
	if !strings.Contains(got, `"replaceBuiltInOptions": true`) {
		t.Errorf("replaceBuiltInOptions should be set:\n%s", got)
	}
	if n := strings.Count(got, `"model":`); n != 2 {
		t.Errorf("expected exactly the 2 brewkeg options, got %d:\n%s", n, got)
	}

	if _, err := b.Restore(false); err != nil {
		t.Fatal(err)
	}
	if after := ReadFile(settings); after != before {
		t.Errorf("restore should put the original picker back:\n got: %s\nwant: %s", after, before)
	}
}

// A picker with no options must not wipe the user's list.
func TestEmptyPickerLeavesFileAlone(t *testing.T) {
	h := home(t)
	settings := filepath.Join(h, ".claude", "settings.json")
	before := `{"modelPicker":{"options":[{"model":"mine"}]}}`
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := DefaultSpec()
	spec.Pickers = map[string]ModelPickerSpec{"claude-cli": {Path: ".claude/settings.json", Replace: true}}
	if _, _, err := ApplyWithSpec(spec, []string{"claude-cli"}, Options{APIKey: "bk_live_K"}); err != nil {
		t.Fatal(err)
	}
	if got := ReadFile(settings); !strings.Contains(got, "mine") {
		t.Errorf("an empty picker must not clear the user's options:\n%s", got)
	}
}
