package brewkeg

import (
	"os"
	"path/filepath"
	"testing"
)

// withTempHome points Home() at a scratch dir. Every path helper goes through
// it, so the whole engine writes inside the test and the real machine is never
// touched.
func withTempHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir) // Windows
	t.Setenv("BREWKEG_NO_RELAUNCH", "1")
	t.Setenv("BREWKEG_BASE_URL", "https://brewkeg.test")
}

// A key given to the app must survive a relaunch. The old behaviour read the
// key back out of ~/.claude/settings.json, which was empty for anyone who had
// only configured Codex.
func TestStoredKeySurvivesForCodexOnlySetup(t *testing.T) {
	withTempHome(t)

	const key = "bk_live_codexonly_1234"
	// Codex is the only target written; ~/.claude is never touched.
	if _, _, err := ApplyWithSpec(DefaultSpec(), []string{"codex"}, Options{APIKey: key}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if got := StoredKey(); got != key {
		t.Fatalf("StoredKey() = %q, want %q", got, key)
	}
	if FileExists(HomeJoin(".claude", "settings.json")) {
		t.Fatal("a Codex-only setup must not write ~/.claude/settings.json")
	}

	// Per the Unix contract: readable only by the owner.
	fi, err := os.Stat(KeyStorePath())
	if err != nil {
		t.Fatalf("stat key store: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("key store mode = %o, want 600", perm)
	}
	if dfi, err := os.Stat(filepath.Dir(KeyStorePath())); err == nil {
		if perm := dfi.Mode().Perm(); perm&0o077 != 0 {
			t.Fatalf("key dir mode = %o, want no group/other bits", perm)
		}
	}
}

// Machines configured by an older build, or by hand, have no key store. The
// Claude settings are still a valid source.
func TestStoredKeyFallsBackToClaudeSettings(t *testing.T) {
	withTempHome(t)

	const key = "bk_live_legacy_9999"
	if err := os.MkdirAll(HomeJoin(".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"env":{"ANTHROPIC_AUTH_TOKEN":"` + key + `","ANTHROPIC_BASE_URL":"https://brewkeg.dev"}}`
	if err := os.WriteFile(HomeJoin(".claude", "settings.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := StoredKey(); got != key {
		t.Fatalf("StoredKey() = %q, want %q", got, key)
	}
}

// The store is authoritative once written: switching tools must not read a
// stale key back out of a config file we are about to rewrite.
func TestKeyStoreWinsOverClaudeSettings(t *testing.T) {
	withTempHome(t)

	os.MkdirAll(HomeJoin(".claude"), 0o755)
	os.WriteFile(HomeJoin(".claude", "settings.json"),
		[]byte(`{"env":{"ANTHROPIC_AUTH_TOKEN":"bk_live_old_key_0001"}}`), 0o644)

	if err := StoreKey("bk_live_new_key_0002"); err != nil {
		t.Fatalf("StoreKey: %v", err)
	}
	if got := StoredKey(); got != "bk_live_new_key_0002" {
		t.Fatalf("StoredKey() = %q, want the stored key", got)
	}

	if err := ClearKey(); err != nil {
		t.Fatalf("ClearKey: %v", err)
	}
	// Cleared means the fallback is visible again, not that the key is gone
	// from the machine.
	if got := StoredKey(); got != "bk_live_old_key_0001" {
		t.Fatalf("after ClearKey StoredKey() = %q, want the legacy fallback", got)
	}
}

// A run where every write failed left the machine untouched, so remembering the
// key would be claiming a state that never existed.
func TestFailedApplyDoesNotStoreKey(t *testing.T) {
	withTempHome(t)

	_, _, err := ApplyWithSpec(DefaultSpec(), []string{"nope"}, Options{APIKey: "bk_live_x"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if FileExists(KeyStorePath()) {
		t.Fatal("a run that wrote nothing must not store the key")
	}
	if got := StoredKey(); got != "" {
		t.Fatalf("StoredKey() = %q, want empty", got)
	}
}

func TestClearKeyIsIdempotent(t *testing.T) {
	withTempHome(t)
	if err := StoreKey("bk_live_abc_0001"); err != nil {
		t.Fatal(err)
	}
	if err := ClearKey(); err != nil {
		t.Fatalf("first ClearKey: %v", err)
	}
	if err := ClearKey(); err != nil {
		t.Fatalf("ClearKey on a missing file should be a no-op, got: %v", err)
	}
}

func TestStoreKeyIgnoresBlank(t *testing.T) {
	withTempHome(t)
	if err := StoreKey("   "); err != nil {
		t.Fatalf("blank key should not error: %v", err)
	}
	if FileExists(KeyStorePath()) {
		t.Fatal("a blank key must not create the store")
	}
}

// Every backup of a tool config is a copy of a file that holds the key, so the
// backup tree is as sensitive as the store itself. This is how it actually
// shipped once: backups went through a plain MkdirAll(0755).
func TestBrewkegHomeIsOwnerOnly(t *testing.T) {
	withTempHome(t)

	const key = "bk_live_permcheck_5555"
	if _, _, err := ApplyWithSpec(DefaultSpec(), []string{"codex"}, Options{APIKey: key}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, d := range []string{brewkegHome(), BackupsRoot()} {
		fi, err := os.Stat(d)
		if err != nil {
			t.Fatalf("stat %s: %v", d, err)
		}
		if perm := fi.Mode().Perm(); perm&0o077 != 0 {
			t.Errorf("%s mode = %o, want owner-only", d, perm)
		}
	}
}

// An existing ~/.brewkeg from an older build was 0755. MkdirAll alone would not
// tighten it, so the chmod is load-bearing.
func TestLooseBrewkegHomeGetsTightened(t *testing.T) {
	withTempHome(t)

	if err := os.MkdirAll(brewkegHome(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(brewkegHome(), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, _, err := ApplyWithSpec(DefaultSpec(), []string{"codex"}, Options{APIKey: "bk_live_loose_0001"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	fi, err := os.Stat(brewkegHome())
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("mode = %o, want the pre-existing 0755 dir tightened", perm)
	}
}
