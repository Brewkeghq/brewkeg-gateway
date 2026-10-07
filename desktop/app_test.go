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
