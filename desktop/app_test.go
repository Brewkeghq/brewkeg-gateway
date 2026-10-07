package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brewkeg/brewkeg-cli/internal/brewkeg"
)

// sandbox points HOME at a temp dir so these run exactly the code the window
// calls, without touching the developer's real ~/.claude and ~/.codex.
//
// It also stands up a gateway that accepts any key: Configure now refuses to
// write a rejected key, so a test that is not about key validation needs a
// reachable gateway or it would be blocked for the wrong reason. A test about
// the gate itself overrides BREWKEG_BASE_URL afterwards.
func sandbox(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_1"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("BREWKEG_NO_RELAUNCH", "1")
	t.Setenv("BREWKEG_BASE_URL", srv.URL)
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

	res := app.Configure("bk_live_testkey123", []string{"claude-cli", "codex"}, state.BaseURL, "", "", "")
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
	res := app.Configure("   ", []string{"codex"}, "https://brewkeg.dev", "", "", "")
	if res.OK || res.Message == "" {
		t.Fatal("a blank key must be refused with a message the window can show")
	}
}

// "Select nothing" used to be refused outright. It is not a dead button any
// more: when something is connected it is the disconnect action, and when
// nothing is there is still refused rather than claiming to have done something.
func TestConfigureRefusesWithNothingSelectedAndNothingConnected(t *testing.T) {
	sandbox(t)
	app := NewApp()
	res := app.Configure("bk_live_testkey123", nil, "https://brewkeg.dev", "", "", "")
	if res.OK || res.Message == "" {
		t.Fatal("selecting no services on a clean machine must be refused, not silently succeed")
	}
}

// The contract the user asked for: untoggle a tool, press the button, and its
// config is cleared. Previously this returned "Turn on at least one service",
// which made unconfiguring the one thing the window could not do.
func TestConfigureWithNothingSelectedEvictsWhatWasOn(t *testing.T) {
	home := sandbox(t)
	app := NewApp()

	if res := app.Configure("bk_live_testkey123", []string{"claude-cli"}, "https://brewkeg.dev", "", "", ""); !res.OK {
		t.Fatal(res.Message)
	}
	settings := filepath.Join(home, ".claude", "settings.json")
	if !strings.Contains(brewkeg.ReadFile(settings), "ANTHROPIC_BASE_URL") {
		t.Fatal("first run did not configure anything to evict")
	}

	res := app.Configure("bk_live_testkey123", nil, "https://brewkeg.dev", "", "", "")
	if !res.OK {
		t.Fatalf("disconnecting must be possible: %s", res.Message)
	}
	if got := brewkeg.ReadFile(settings); strings.Contains(got, "ANTHROPIC_BASE_URL") || strings.Contains(got, "bk_live_") {
		t.Fatalf("brewkeg survived the disconnect:\n%s", got)
	}
	if got := brewkeg.ReadFile(settings); !strings.Contains(got, "theme") {
		t.Fatalf("evict destroyed something that is not ours:\n%s", got)
	}
	if !strings.Contains(res.Message, "Disconnect") {
		t.Fatalf("the window would tell the user the wrong direction: %q", res.Message)
	}

	for _, tg := range app.GetState().Targets {
		if tg.Enabled {
			t.Fatalf("%s still reports as enabled after a disconnect", tg.ID)
		}
	}
}

// Disconnecting must not require a key. Once every tool is pointed away from us
// there is nothing left to authenticate, and demanding a secret to undo our own
// edits locks out exactly the user whose key has just expired.
func TestConfigureDisconnectsWithNoKeyAtAll(t *testing.T) {
	home := sandbox(t)
	app := NewApp()

	if res := app.Configure("bk_live_testkey123", []string{"claude-cli"}, "https://brewkeg.dev", "", "", ""); !res.OK {
		t.Fatal(res.Message)
	}
	if err := os.Remove(filepath.Join(home, ".brewkeg", "key")); err != nil {
		t.Fatalf("could not clear the key store: %v", err)
	}

	res := app.Configure("", nil, "https://brewkeg.dev", "", "", "")
	if !res.OK {
		t.Fatalf("disconnect must not be gated on a key: %s", res.Message)
	}
	if got := brewkeg.ReadFile(filepath.Join(home, ".claude", "settings.json")); strings.Contains(got, "bk_live_") {
		t.Fatalf("key still in the config after a keyless disconnect:\n%s", got)
	}
	if res.Warning != "" {
		t.Fatalf("a keyless disconnect has no untested key to warn about: %q", res.Warning)
	}
}

// Claude Desktop is half-writable. The developer switch is a plain JSON file we
// own and must write for the user; the gateway lives in a GUI store we cannot
// read, so that half is still printed as steps. This test keeps the boundary
// honest in both directions.
func TestDesktopWritesDevToolsSwitchButStillPrintsGatewaySteps(t *testing.T) {
	home := sandbox(t)
	dev := filepath.Join(home, "Library", "Application Support", "Claude", "developer_settings.json")
	if err := os.MkdirAll(filepath.Dir(dev), 0o755); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	res := app.Configure("bk_live_testkey123", []string{"desktop"}, "https://brewkeg.dev", "", "", "")
	if !res.OK {
		t.Fatalf("desktop should succeed: %s", res.Message)
	}

	for _, r := range res.Results {
		if r.Manual == "" {
			t.Error("the gateway half is still a GUI — it must print steps")
		}
		if !strings.Contains(r.Manual, "Configure Third-Party Inference") {
			t.Errorf("manual steps lost their instruction: %q", r.Manual)
		}
	}

	if !brewkeg.FileExists(dev) {
		t.Fatalf("allowDevTools should have been written to %s", dev)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(readFileString(t, dev)), &got); err != nil {
		t.Fatalf("developer_settings.json is not valid JSON: %v", err)
	}
	// A string "true" is not a boolean to Electron, and it would fail silently.
	if v, ok := got["allowDevTools"].(bool); !ok || !v {
		t.Errorf("allowDevTools = %#v, want the boolean true", got["allowDevTools"])
	}
}

// The switch must not swallow anything the user put there themselves.
func TestDesktopDevToolsMergeIsSurgical(t *testing.T) {
	home := sandbox(t)
	dev := filepath.Join(home, "Library", "Application Support", "Claude", "developer_settings.json")
	if err := os.MkdirAll(filepath.Dir(dev), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "{\n  \"someOtherSetting\": 42,\n  \"nested\": {\"keep\": true}\n}\n"
	if err := os.WriteFile(dev, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	if res := app.Configure("bk_live_testkey123", []string{"desktop"}, "https://brewkeg.dev", "", "", ""); !res.OK {
		t.Fatal(res.Message)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(readFileString(t, dev)), &got); err != nil {
		t.Fatal(err)
	}
	if got["someOtherSetting"] != float64(42) {
		t.Errorf("unrelated key was changed: %#v", got["someOtherSetting"])
	}
	if _, ok := got["nested"].(map[string]any); !ok {
		t.Errorf("nested object was lost: %#v", got["nested"])
	}
	if got["allowDevTools"] != true {
		t.Errorf("allowDevTools was not set: %#v", got)
	}

	// And undo puts their file back byte for byte.
	if _, err := brewkeg.LatestBackup(); err == nil {
		if b, err := brewkeg.LatestBackup(); err == nil {
			if _, err := b.Restore(false); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := readFileString(t, dev); got != original {
		t.Errorf("restore was not byte-exact:\ngot:  %q\nwant: %q", got, original)
	}
}

// Windows and Linux put Claude Desktop somewhere else entirely. One spec has
// to cover all three or the app silently does nothing on two platforms.
func TestDesktopPathIsCorrectPerOS(t *testing.T) {
	var target brewkeg.TargetSpec
	for _, t := range brewkeg.DefaultSpec().Targets {
		if t.ID == "desktop" {
			target = t
		}
	}
	if len(target.Files) != 2 {
		t.Fatalf("desktop should write two files, got %d", len(target.Files))
	}
	f := target.Files[1] // the developer-mode switch; Files[0] is the 3p config

	got := map[string]string{
		"darwin":  f.Paths["darwin"],
		"windows": f.Paths["windows"],
		"linux":   f.Paths["linux"],
	}
	want := map[string]string{
		"darwin":  "Library/Application Support/Claude/developer_settings.json",
		"windows": "AppData/Roaming/Claude/developer_settings.json",
		"linux":   ".config/Claude/developer_settings.json",
	}
	// Windows is %LOCALAPPDATA% for the *inference* config — not %APPDATA%.
	// Check the support dir the engine resolves for that, separately.
	for os_, path := range want {
		if got[os_] != path {
			t.Errorf("%s path = %q, want %q", os_, got[os_], path)
		}
		if strings.HasPrefix(got[os_], "/") {
			t.Errorf("%s path %q is absolute — that would need sudo and writes outside $HOME", os_, got[os_])
		}
		// Every path must land inside $HOME: an absolute or escaping path
		// would need sudo and could write somewhere the user never agreed to.
		if !strings.HasPrefix(filepath.Clean(brewkeg.HomeJoin(got[os_])), filepath.Clean(brewkeg.Home())) {
			t.Errorf("%s path %q resolves outside $HOME", os_, got[os_])
		}
	}
	if f.Kind != "json-plain" {
		t.Errorf("kind = %q, want json-plain", f.Kind)
	}
	if !strings.HasSuffix(brewkeg.ClaudeDesktopConfigDir(), filepath.Join("configLibrary")) {
		t.Errorf("config dir = %q, want it to end in configLibrary", brewkeg.ClaudeDesktopConfigDir())
	}
}

func TestGetStateReportsEnabledAfterConfigure(t *testing.T) {
	sandbox(t)
	app := NewApp()
	if res := app.Configure("bk_live_testkey123", []string{"claude-cli"}, "https://brewkeg.dev", "", "", ""); !res.OK {
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
	if res := app.Configure(key, []string{"claude-cli"}, "https://brewkeg.dev", "", "", ""); !res.OK {
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
	if res := app.Configure(key, []string{"codex"}, "https://brewkeg.dev", "", "", ""); !res.OK {
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
	if res := app.Configure(key, []string{"claude-cli"}, "https://brewkeg.dev", "", "", ""); !res.OK {
		t.Fatal(res.Message)
	}
	// Pretend the user cleared the field and hit the button again.
	res := app.Configure("", []string{"claude-cli"}, "https://brewkeg.dev", "", "", "")
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

// The headline rule: a key the gateway refuses must never reach a config file.
// The config would then be pointing every tool at an auth error, and the
// backup we took is a backup of a working setup we have already replaced.
func TestRejectedKeyWritesNothing(t *testing.T) {
	home := sandbox(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	t.Setenv("BREWKEG_BASE_URL", srv.URL)

	app := NewApp()
	res := app.Configure("bk_live_definitely_wrong", []string{"claude-cli", "codex"}, srv.URL, "", "", "")

	if res.OK {
		t.Fatal("a rejected key must not be allowed to save")
	}
	if !strings.Contains(res.Message, "rejected") {
		t.Errorf("message = %q, want it to say the key was rejected", res.Message)
	}

	if got := readFileString(t, filepath.Join(home, ".claude", "settings.json")); got != "{\n  \"theme\": \"dark\"\n}\n" {
		t.Errorf("settings.json was modified by a rejected key:\n%s", got)
	}
	want := "model = \"gpt-5\"\n\n[tui]\ntheme = \"dark\"\n"
	if got := readFileString(t, filepath.Join(home, ".codex", "config.toml")); got != want {
		t.Errorf("config.toml was modified by a rejected key:\n%s", got)
	}
	if brewkeg.FileExists(brewkeg.KeyStorePath()) {
		t.Error("a rejected key must not be remembered")
	}
	if brewkeg.FileExists(brewkeg.BackupsRoot()) {
		t.Error("nothing was written, so there is nothing to back up")
	}
}

// A gateway outage must not read as a bad key. The write proceeds, loudly.
func TestUnreachableGatewayStillWrites(t *testing.T) {
	home := sandbox(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing listening: no verdict available

	t.Setenv("BREWKEG_BASE_URL", url)
	app := NewApp()

	res := app.Configure("bk_live_untested", []string{"codex"}, url, "", "", "")
	if !res.OK {
		t.Fatalf("an outage must not block setup: %s", res.Message)
	}
	if res.Warning == "" {
		t.Error("saving an untested key must say so")
	}
	if !strings.Contains(readFileString(t, filepath.Join(home, ".codex", "config.toml")), "bk_live_untested") {
		t.Error("the config should still have been written")
	}
}

// 429 is the gateway saying "valid key, no credit" — a different problem from a
// wrong key, and it must not stop someone wiring up a tool they will top up
// later.
func TestOutOfQuotaKeyIsStillWritten(t *testing.T) {
	home := sandbox(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"quota exhausted"}}`))
	}))
	defer srv.Close()
	t.Setenv("BREWKEG_BASE_URL", srv.URL)

	app := NewApp()
	res := app.Configure("bk_live_valid_nocredit", []string{"codex"}, srv.URL, "", "", "")
	if !res.OK {
		t.Fatalf("a valid key with no quota should still save: %s", res.Message)
	}
	if res.Warning != "" {
		t.Errorf("warning = %q, want none — the key was proven good", res.Warning)
	}
	if !strings.Contains(readFileString(t, filepath.Join(home, ".codex", "config.toml")), "bk_live_valid_nocredit") {
		t.Error("config should have been written")
	}
}
