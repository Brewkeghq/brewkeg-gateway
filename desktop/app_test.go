package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brewkeghq/brewkeg-gateway/internal/brewkeg"
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
	if len(state.Targets) != len(brewkeg.DefaultSpec().Targets) {
		t.Fatalf("every spec target should be reported, got %d", len(state.Targets))
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

// A target that succeeds while still needing the user by hand must not be
// bounced. Bouncing an app for the half we could write presents as "it
// restarted but nothing happened" — which is exactly what happened.
//
// This is a unit test of `finished`, not of the desktop target's manual steps.
// It used to drive the real desktop target and assert it still had steps — a
// premise that stopped being true when we made Claude Desktop fully writable,
// so the test went red for the right reason at the wrong layer. The guard lives
// in the filter, so the filter is what gets tested.
func TestFinishedSkipsAnythingTheUserStillHasToDo(t *testing.T) {
	got := finished([]brewkeg.ApplyResult{
		// Done: written, moved bytes, nothing left by hand.
		{ID: "codex", OK: true, Changed: true, Paths: []string{"~/.codex/config.toml"}},
		// Done but rewritten byte-identically — nothing to apply, so nothing to
		// restart. This is the fleet-restart bug in one line.
		{ID: "claude-cli", OK: true, Changed: false, Paths: []string{"~/.claude/settings.json"}},
		// Succeeded, wrote a prerequisite, but the real work is manual.
		{ID: "desktop", OK: true, Changed: true, Paths: []string{"developer_settings.json"},
			Manual: "Open Developer Mode, then reopen the app"},
		// Nothing was written at all.
		{ID: "zcode", OK: true, Changed: true},
		// Failed.
		{ID: "broken", OK: false, Changed: true, Paths: []string{"~/.x"}},
	})

	want := []string{"codex"}
	if len(got) != len(want) {
		t.Fatalf("finished() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("finished() = %v, want %v", got, want)
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

// homeOf is the sandbox this test is running in. t.Setenv values are only
// readable through t, so every helper needs it passed rather than looked up.
func homeOf(t *testing.T) string {
	t.Helper()
	return os.Getenv("HOME")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("reading %s: %v", p, err)
	}
	return string(raw)
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

// Toggling one service must not bounce the whole fleet.
//
// The window sends the full desired set on every toggle, so a run for one
// target rewrites the others too. Before this was gated on the write actually
// changing bytes, every enabled target reported itself as finished and Claude,
// Codex and ZCode were all quit and relaunched for a change to one of them.
func TestReapplyingAnUnchangedTargetRestartsNothing(t *testing.T) {
	sandbox(t)
	app := NewApp()

	both := []string{"claude-cli", "codex"}
	if res := app.Configure("bk_live_fleet_1", both, "https://brewkeg.dev", "", "", ""); !res.OK {
		t.Fatal(res.Message)
	}

	// Second run with the same values: nothing should be reported as changed.
	res := app.Configure("bk_live_fleet_1", both, "https://brewkeg.dev", "", "", "")
	if !res.OK {
		t.Fatal(res.Message)
	}
	for _, r := range res.Results {
		if r.Changed {
			t.Fatalf("%s reported changed on an identical re-run: %+v", r.ID, r)
		}
	}
	if len(res.Restart) != 0 {
		t.Fatalf("an identical re-run scheduled restarts: %+v", res.Restart)
	}

	// And the guard must not disable the feature: one real change still
	// schedules exactly that one target.
	changed := app.Configure("bk_live_fleet_2", both, "https://brewkeg.dev", "", "", "")
	if !changed.OK {
		t.Fatal(changed.Message)
	}
	var touched []string
	for _, r := range changed.Results {
		if r.Changed {
			touched = append(touched, r.ID)
		}
	}
	if len(touched) != 2 {
		t.Fatalf("a new key must change both targets, got %v", touched)
	}
	var hints []string
	for _, h := range changed.Restart {
		hints = append(hints, h.What)
	}
	joined := strings.Join(hints, "|")
	for _, want := range []string{"Claude Code", "Codex CLI"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("%s changed but was not scheduled: %+v", want, changed.Restart)
		}
	}
}

// A removal has to survive the same filter: taking brewkeg out moves bytes,
// so the tool must still be told to restart.
func TestRemovingATargetStillReportsChanged(t *testing.T) {
	sandbox(t)
	app := NewApp()

	both := []string{"claude-cli", "codex"}
	if res := app.Configure("bk_live_remove_1", both, "https://brewkeg.dev", "", "", ""); !res.OK {
		t.Fatal(res.Message)
	}
	res := app.Configure("bk_live_remove_1", []string{"claude-cli"}, "https://brewkeg.dev", "", "", "")
	if !res.OK {
		t.Fatal(res.Message)
	}
	var removed bool
	for _, r := range res.Results {
		if r.ID == "codex" && r.Removed && r.Changed {
			removed = true
		}
	}
	if !removed {
		t.Fatalf("evicting codex did not report a change: %+v", res.Results)
	}
}

// Reset is the escape hatch every user eventually needs: point every tool away
// from us and forget the key. It has to be complete — a reset that leaves one
// service configured is worse than none, because status then reports the tool
// as connected while the user believes they are done.
func TestResetDisconnectsEverythingAndForgetsTheKey(t *testing.T) {
	sandbox(t)
	app := NewApp()

	both := []string{"claude-cli", "codex"}
	if res := app.Configure("bk_live_reset_1", both, "https://brewkeg.dev", "", "", ""); !res.OK {
		t.Fatal(res.Message)
	}
	if !fileExists(filepath.Join(homeOf(t), ".brewkeg", "key")) {
		t.Fatal("configure should have stored the key")
	}

	res := app.Reset(false)
	if !strings.Contains(res.Message, "Disconnected") {
		t.Fatalf("reset should say what it disconnected, got %q", res.Message)
	}
	if res.BackupID == "" {
		t.Fatal("a reset must save a backup — it overwrites user files")
	}
	for _, r := range res.Results {
		if !r.Removed {
			t.Fatalf("%s was not disconnected by the reset: %+v", r.ID, r)
		}
	}

	// Nothing may still be pointed at us.
	st := app.GetState()
	for _, tg := range st.Targets {
		if tg.Enabled {
			t.Fatalf("%s still reports enabled after a reset", tg.ID)
		}
	}
	if fileExists(filepath.Join(homeOf(t), ".brewkeg", "key")) {
		t.Fatal("reset must forget the stored key")
	}
	if st.ApiKey != "" || st.HasKey {
		t.Fatalf("state still carries a key after a reset: %q", st.ApiKey)
	}
}

// A dry run describes the reset and writes nothing — that is what the menu
// confirmation is built from, so a plan that acted would act twice.
func TestResetDryRunChangesNothing(t *testing.T) {
	sandbox(t)
	app := NewApp()

	if res := app.Configure("bk_live_reset_2", []string{"codex"}, "https://brewkeg.dev", "", "", ""); !res.OK {
		t.Fatal(res.Message)
	}
	before := readFile(t, filepath.Join(homeOf(t), ".codex", "config.toml"))

	res := app.Reset(true)
	if res.BackupID != "" {
		t.Fatalf("a dry run must not save a backup, got %s", res.BackupID)
	}
	if got := readFile(t, filepath.Join(homeOf(t), ".codex", "config.toml")); got != before {
		t.Fatal("a dry run rewrote a config file")
	}
	if !fileExists(filepath.Join(homeOf(t), ".brewkeg", "key")) {
		t.Fatal("a dry run must not forget the key")
	}

	st := app.GetState()
	for _, tg := range st.Targets {
		if tg.ID == "codex" && !tg.Enabled {
			t.Fatal("a dry run disconnected codex")
		}
	}
}
