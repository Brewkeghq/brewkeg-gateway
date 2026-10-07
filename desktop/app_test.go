package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brewkeg/brewkeg-cli/internal/brewkeg"
)

// sandbox points HOME at a temp dir so these run exactly the code the window
// calls, without touching the developer's real ~/.claude and ~/.codex.
func sandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SHELL", "/bin/zsh")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("model = \"gpt-5\"\n\n[tui]\ntheme = \"dark\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte("{\n  \"theme\": \"dark\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestConfigureThenRestoreIsByteExact(t *testing.T) {
	home := sandbox(t)
	app := NewApp()

	state := app.GetState()
	if len(state.Targets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(state.Targets))
	}

	res := app.Configure("bk_live_testkey123", []string{"claude-cli", "codex"}, state.BaseURL, "", "")
	if !res.OK {
		t.Fatalf("configure failed: %s (%v)", res.Message, res.Results)
	}
	if res.BackupID == "" {
		t.Fatal("no backup id reported — the window would have no undo")
	}

	toml := brewkeg.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(toml, "[model_providers.brewkeg]") || !strings.Contains(toml, "bk_live_testkey123") {
		t.Fatalf("codex config was not written:\n%s", toml)
	}
	settings := brewkeg.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if !strings.Contains(settings, "ANTHROPIC_BASE_URL") {
		t.Fatalf("claude settings were not written:\n%s", settings)
	}

	if _, err := app.Restore(res.BackupID); err != nil {
		t.Fatal(err)
	}

	if got := brewkeg.ReadFile(filepath.Join(home, ".codex", "config.toml")); got != "model = \"gpt-5\"\n\n[tui]\ntheme = \"dark\"\n" {
		t.Fatalf("codex config not restored byte-exact:\n%q", got)
	}
	if got := brewkeg.ReadFile(filepath.Join(home, ".claude", "settings.json")); got != "{\n  \"theme\": \"dark\"\n}\n" {
		t.Fatalf("claude settings not restored byte-exact:\n%q", got)
	}
	if brewkeg.FileExists(filepath.Join(home, ".zshrc")) {
		t.Fatal("the shell rc brewkeg created was not removed on restore")
	}
}

func TestConfigureRefusesWithoutKey(t *testing.T) {
	sandbox(t)
	app := NewApp()
	res := app.Configure("   ", []string{"codex"}, "https://brewkeg.dev", "", "")
	if res.OK || res.Message == "" {
		t.Fatal("a blank key must be refused with a message the window can show")
	}
}

func TestConfigureRefusesWithNothingSelected(t *testing.T) {
	sandbox(t)
	app := NewApp()
	res := app.Configure("bk_live_testkey123", nil, "https://brewkeg.dev", "", "")
	if res.OK || res.Message == "" {
		t.Fatal("selecting no services must be refused, not silently succeed")
	}
}

func TestDesktopTargetNeverWritesAFile(t *testing.T) {
	home := sandbox(t)
	app := NewApp()
	res := app.Configure("bk_live_testkey123", []string{"desktop"}, "https://brewkeg.dev", "", "")
	if !res.OK {
		t.Fatalf("desktop should succeed with instructions: %s", res.Message)
	}
	for _, r := range res.Results {
		if r.Manual == "" {
			t.Fatal("desktop must return manual steps — it has no config file we own")
		}
		if len(r.Paths) > 0 {
			t.Fatalf("desktop must not claim to write files, got %v", r.Paths)
		}
	}
	if brewkeg.FileExists(filepath.Join(home, "Library", "Application Support", "Claude", "config.json")) {
		t.Fatal("desktop target wrote a file it does not own")
	}
}

func TestGetStateReportsEnabledAfterConfigure(t *testing.T) {
	sandbox(t)
	app := NewApp()
	if res := app.Configure("bk_live_testkey123", []string{"claude-cli"}, "https://brewkeg.dev", "", ""); !res.OK {
		t.Fatal(res.Message)
	}
	state := app.GetState()
	for _, tg := range state.Targets {
		if tg.ID != "claude-cli" {
			continue
		}
		if !tg.Enabled {
			t.Fatal("state should report claude-cli as on after a successful configure")
		}
	}
}

// Relaunching the app must not cost the user the key they already gave us:
// GetState hands it straight back so the field opens filled in.
func TestGetStateReturnsTheStoredKey(t *testing.T) {
	sandbox(t)
	app := NewApp()

	if st := app.GetState(); st.ApiKey != "" || st.HasKey {
		t.Fatalf("fresh sandbox should have no key, got hasKey=%v apiKey=%q", st.HasKey, st.ApiKey)
	}

	const key = "bk_live_prefill_4242"
	if res := app.Configure(key, []string{"claude-cli"}, "https://brewkeg.dev", "", ""); !res.OK {
		t.Fatal(res.Message)
	}

	st := app.GetState()
	if !st.HasKey {
		t.Fatal("HasKey should be true after a successful configure")
	}
	if st.ApiKey != key {
		t.Fatalf("ApiKey = %q, want the key verbatim", st.ApiKey)
	}
	if !strings.Contains(st.MaskedKey, "*") {
		t.Fatalf("MaskedKey = %q, want it masked", st.MaskedKey)
	}
}

// Codex-only is the case the old read-from-settings.json could not serve: the
// key was written to ~/.codex and nowhere else.
func TestPrefillWorksForCodexOnly(t *testing.T) {
	sandbox(t)
	app := NewApp()

	const key = "bk_live_codex_prefill_77"
	if res := app.Configure(key, []string{"codex"}, "https://brewkeg.dev", "", ""); !res.OK {
		t.Fatal(res.Message)
	}
	if st := app.GetState(); st.ApiKey != key {
		t.Fatalf("ApiKey = %q, want %q", st.ApiKey, key)
	}
}

// An empty box on relaunch must not be a dead end: the remembered key fills in
// rather than the window refusing to do anything.
func TestConfigureFallsBackToTheStoredKey(t *testing.T) {
	sandbox(t)
	app := NewApp()

	const key = "bk_live_fallback_3131"
	if res := app.Configure(key, []string{"claude-cli"}, "https://brewkeg.dev", "", ""); !res.OK {
		t.Fatal(res.Message)
	}
	// Pretend the user cleared the field and hit the button again.
	res := app.Configure("", []string{"claude-cli"}, "https://brewkeg.dev", "", "")
	if !res.OK {
		t.Fatalf("empty field should fall back to the stored key, got: %s", res.Message)
	}
	if !strings.Contains(readFileString(t, filepath.Join(sandboxHOME(t), ".claude", "settings.json")), key) {
		t.Fatal("the fallback key should be what landed in settings.json")
	}
}

func readFileString(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func sandboxHOME(t *testing.T) string {
	t.Helper()
	return os.Getenv("HOME")
}
