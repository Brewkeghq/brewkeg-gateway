package brewkeg

import (
	"os"
	"testing"
)

func TestUserConfigRoundTrip(t *testing.T) {
	withTempHome(t)

	in := UserConfig{APIKey: "bk_live_cfg_0001", Targets: []string{"codex"}, BaseURL: "https://gw.test/"}
	if err := SaveUserConfig(in); err != nil {
		t.Fatal(err)
	}
	got := LoadUserConfig()
	if got.APIKey != in.APIKey || got.BaseURL != "https://gw.test" || len(got.Targets) != 1 {
		t.Fatalf("round trip lost something: %+v", got)
	}

	fi, err := os.Stat(UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("config mode = %o, want 600 — it usually holds the key", perm)
	}
}

// Precedence is the whole contract: flags beat the file, the file beats a
// prompt. An override must only replace what it names.
func TestFlagsBeatConfigFile(t *testing.T) {
	withTempHome(t)

	cfg := UserConfig{
		APIKey:    "bk_live_from_file",
		Targets:   []string{"codex"},
		BaseURL:   "https://file.test",
		Model:     "claude-opus-5",
		FastModel: "claude-haiku-4-5",
	}

	got := cfg.Resolve(Options{})
	if got.APIKey != "bk_live_from_file" || got.BaseURL != "https://file.test" {
		t.Fatalf("empty options should inherit the file, got %+v", got)
	}

	got = cfg.Resolve(Options{APIKey: "bk_live_from_flag"})
	if got.APIKey != "bk_live_from_flag" {
		t.Errorf("a flag must win over the file, got %q", got.APIKey)
	}
	if got.BaseURL != "https://file.test" {
		t.Errorf("overriding the key must not clear the base url, got %q", got.BaseURL)
	}
}

// A file written for an older release must not break a newer one.
func TestConfiguredTargetsDropsUnknownIds(t *testing.T) {
	withTempHome(t)
	if err := SaveUserConfig(UserConfig{
		APIKey:  "bk_live_x",
		Targets: []string{"codex", "a-tool-that-was-removed", "", "claude-cli"},
	}); err != nil {
		t.Fatal(err)
	}

	got := ConfiguredTargets(DefaultSpec())
	if len(got) != 2 || got[0] != "codex" || got[1] != "claude-cli" {
		t.Fatalf("targets = %v, want the two known ids in order", got)
	}
}

func TestMissingUserConfigIsNotAnError(t *testing.T) {
	withTempHome(t)
	got := LoadUserConfig()
	if got.APIKey != "" || len(ConfiguredTargets(DefaultSpec())) != 0 {
		t.Fatalf("no file should read as empty, got %+v", got)
	}
}

// A hand-edited file with a syntax error must read as empty, never as a
// half-parsed object that would silently configure the wrong thing.
func TestMalformedUserConfigReadsAsEmpty(t *testing.T) {
	withTempHome(t)
	if err := os.MkdirAll(brewkegHome(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(UserConfigPath(), []byte(`{"apiKey": "bk_live_x", "targets":`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := LoadUserConfig()
	if got.APIKey != "" || len(got.Targets) != 0 {
		t.Fatalf("malformed file should read as empty, got %+v", got)
	}
	if len(ConfiguredTargets(DefaultSpec())) != 0 {
		t.Fatal("malformed file must not name any targets")
	}
}

func TestLabelForFallsBackToTheId(t *testing.T) {
	withTempHome(t)
	if l := LabelFor(DefaultSpec(), "codex"); l != "Codex CLI" {
		t.Errorf("LabelFor(codex) = %q, want the spec's label so output matches the window", l)
	}
	if l := LabelFor(DefaultSpec(), "not-a-real-target"); l != "not-a-real-target" {
		t.Errorf("unknown id should fall back to itself, got %q", l)
	}
}
