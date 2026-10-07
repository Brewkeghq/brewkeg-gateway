package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brewkeg/brewkeg-cli/internal/brewkeg"
)

// headlessSandbox is sandbox() plus a config file naming targets, which is the
// only input `gateway --apply` takes.
func headlessSandbox(t *testing.T, targets []string) string {
	home := sandbox(t)
	if err := brewkeg.SaveUserConfig(brewkeg.UserConfig{
		APIKey:  "bk_live_headless_0001",
		Targets: targets,
	}); err != nil {
		t.Fatal(err)
	}
	return home
}

// pinning lets two sandboxes share one gateway, so the only difference between
// their config files can be the thing under test.
func pinGateway(t *testing.T, to string) {
	t.Helper()
	t.Setenv("BREWKEG_BASE_URL", to)
}

func captureStdout(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := fn()
	w.Close()
	os.Stdout = old
	buf := make([]byte, 64*1024)
	n, _ := r.Read(buf)
	return code, string(buf[:n])
}

// The promise: a script and a click produce the same config file. If they can
// drift, the window becomes the thing nobody trusts.
func TestHeadlessAndWindowProduceIdenticalConfig(t *testing.T) {
	const key = "bk_live_same_0001"

	// 1. The window path.
	homeWindow := sandbox(t)
	gateway := os.Getenv("BREWKEG_BASE_URL")
	app := NewApp()
	if res := app.Configure(key, []string{"codex"}, gateway, "", ""); !res.OK {
		t.Fatal(res.Message)
	}
	windowToml := readFileString(t, filepath.Join(homeWindow, ".codex", "config.toml"))

	// 2. The headless path, from a config file alone — same key, same gateway,
	// so any difference in the output is a real difference in behaviour.
	homeHeadless := headlessSandbox(t, []string{"codex"})
	// Pin after the sandbox: sandbox() starts its own gateway, and the last
	// t.Setenv wins.
	pinGateway(t, gateway)
	if err := brewkeg.SaveUserConfig(brewkeg.UserConfig{
		APIKey:  key,
		Targets: []string{"codex"},
	}); err != nil {
		t.Fatal(err)
	}
	code, out := captureStdout(t, func() int { return headless() })
	if code != 0 {
		t.Fatalf("--apply exited %d:\n%s", code, out)
	}
	headlessToml := readFileString(t, filepath.Join(homeHeadless, ".codex", "config.toml"))

	if windowToml != headlessToml {
		t.Fatalf("the two paths disagree.\nwindow:\n%s\nheadless:\n%s", windowToml, headlessToml)
	}
	if !strings.Contains(headlessToml, key) {
		t.Errorf("headless did not write the key:\n%s", headlessToml)
	}
}

// --undo must put back exactly what was there before --apply touched it.
func TestHeadlessApplyThenUndoIsByteExact(t *testing.T) {
	home := headlessSandbox(t, []string{"codex"})
	original := readFileString(t, filepath.Join(home, ".codex", "config.toml"))

	if code, out := captureStdout(t, func() int { return headless() }); code != 0 {
		t.Fatalf("--apply exited %d:\n%s", code, out)
	}
	if readFileString(t, filepath.Join(home, ".codex", "config.toml")) == original {
		t.Fatal("--apply changed nothing, so this proves nothing")
	}
	if code, out := captureStdout(t, func() int { return headlessUndo() }); code != 0 {
		t.Fatalf("--undo exited %d:\n%s", code, out)
	}
	if got := readFileString(t, filepath.Join(home, ".codex", "config.toml")); got != original {
		t.Fatalf("undo was not byte-exact.\ngot:\n%q\nwant:\n%q", got, original)
	}
}

// A rejected key must stop --apply exactly as it stops the window.
func TestHeadlessRefusesARejectedKey(t *testing.T) {
	home := sandbox(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	t.Setenv("BREWKEG_BASE_URL", srv.URL)

	if err := brewkeg.SaveUserConfig(brewkeg.UserConfig{
		APIKey:  "bk_live_wrong",
		Targets: []string{"codex"},
	}); err != nil {
		t.Fatal(err)
	}
	original := readFileString(t, filepath.Join(home, ".codex", "config.toml"))

	code, _ := captureStdout(t, func() int { return headless() })
	if code == 0 {
		t.Fatal("--apply must not succeed with a rejected key")
	}
	if got := readFileString(t, filepath.Join(home, ".codex", "config.toml")); got != original {
		t.Fatalf("a rejected key wrote to the config:\n%s", got)
	}
}

// No config file and no stored key: say so and touch nothing, rather than
// opening a window or half-configuring a machine.
func TestHeadlessWithoutAKeyChangesNothing(t *testing.T) {
	home := sandbox(t)
	original := readFileString(t, filepath.Join(home, ".codex", "config.toml"))

	code, _ := captureStdout(t, func() int { return headless() })
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if got := readFileString(t, filepath.Join(home, ".codex", "config.toml")); got != original {
		t.Fatal("a run with no key must not touch the config")
	}
}

// Targets in the file that the spec no longer knows are dropped, not fatal —
// a config written for an older release should still do the useful part.
func TestHeadlessIgnoresUnknownTargets(t *testing.T) {
	headlessSandbox(t, []string{"retired-tool", "codex"})
	code, out := captureStdout(t, func() int { return headless() })
	if code != 0 {
		t.Fatalf("--apply exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "Codex CLI") {
		t.Errorf("output should name what it configured:\n%s", out)
	}
}
