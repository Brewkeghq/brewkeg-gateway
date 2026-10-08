package brewkeg

import (
	"os"
	"testing"
)

// The override has to beat production, because a developer pointing the app at
// localhost and getting brewkeg.dev back would have no way to tell why their
// change did not show up.
func TestDevBaseURLOverridesProduction(t *testing.T) {
	withTempHome(t)
	// withTempHome pins BREWKEG_BASE_URL to a test host. That variable is the
	// stronger of the two by design, so these tests must clear it or they would
	// be asserting the precedence rule against itself.
	t.Setenv("BREWKEG_BASE_URL", "")

	if err := SetDevBaseURL("http://localhost:3000"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := BaseURL(); got != "http://localhost:3000" {
		t.Fatalf("BaseURL() = %q, want the override", got)
	}
	if !DevModeOn() {
		t.Error("storing a URL must turn developer mode on")
	}
}

// The environment is a deliberate act — launching the app for this one run —
// so it outranks a setting left over from a previous session.
func TestEnvBeatsTheDevOverride(t *testing.T) {
	withTempHome(t)

	if err := SetDevBaseURL("http://localhost:3000"); err != nil {
		t.Fatalf("set: %v", err)
	}
	t.Setenv("BREWKEG_BASE_URL", "http://127.0.0.1:9999")

	if got := BaseURL(); got != "http://127.0.0.1:9999" {
		t.Fatalf("BaseURL() = %q, want the environment to win", got)
	}
}

// Turning the mode off has to actually return the app to production. A toggle
// that only changes the checkbox and leaves the URL in force is the worst
// outcome: the window says brewkeg.dev and the configs get staging.
func TestTurningDevModeOffReturnsToProduction(t *testing.T) {
	withTempHome(t)
	t.Setenv("BREWKEG_BASE_URL", "")

	if err := SetDevBaseURL("http://localhost:3000"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := SetDevMode(false); err != nil {
		t.Fatalf("off: %v", err)
	}
	if got := BaseURL(); got != "https://brewkeg.dev" {
		t.Fatalf("BaseURL() = %q, want production", got)
	}
	// …and the typed URL survives, so a developer comparing the two does not
	// have to retype it.
	if got := ReadDevSettings().BaseURL; got != "http://localhost:3000" {
		t.Fatalf("stored URL = %q, want it kept", got)
	}
}

// A URL that cannot be resolved by any client must never reach a tool's
// config. "localhost:3000" parses with "localhost" as the scheme, which is the
// trap: accepted silently, it writes a base_url nothing can resolve.
func TestBadDevBaseURLsAreRejected(t *testing.T) {
	withTempHome(t)
	t.Setenv("BREWKEG_BASE_URL", "")

	for _, bad := range []string{
		"localhost:3000",  // no scheme
		"ftp://localhost", // not http
		"https://",        // no host
		"http://x/?a=1",   // query string
		"http://x/#frag",  // fragment
	} {
		if err := SetDevBaseURL(bad); err == nil {
			t.Errorf("SetDevBaseURL(%q) was accepted", bad)
		}
		if got := BaseURL(); got != "https://brewkeg.dev" {
			t.Fatalf("a rejected URL changed the gateway to %q", got)
		}
	}
}

// Empty is not a bad URL — it is the panel's "go back to brewkeg.dev", and it
// must clear the override rather than store one with nothing in it.
func TestEmptyDevBaseURLClearsTheOverride(t *testing.T) {
	withTempHome(t)
	t.Setenv("BREWKEG_BASE_URL", "")

	if err := SetDevBaseURL("http://localhost:3000"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := SetDevBaseURL(""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := BaseURL(); got != "https://brewkeg.dev" {
		t.Fatalf("BaseURL() = %q, want production after clearing", got)
	}
	if DevModeOn() {
		t.Error("developer mode is still on with nothing to point at")
	}
}

// The file is hand-editable, so a bad edit has to fail closed. A developer
// debugging should not find their tools pointed at production with no way to
// tell, nor have one typo in developer.json brick the app.
func TestHandEditedDevFileFailsClosed(t *testing.T) {
	withTempHome(t)
	t.Setenv("BREWKEG_BASE_URL", "")

	write := func(body string) {
		if err := os.MkdirAll(brewkegHome(), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(DevSettingsPath(), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write(`{"enabled":true,"baseUrl":"not a url"}`)
	if got := BaseURL(); got != "https://brewkeg.dev" {
		t.Errorf("a malformed URL was honoured: %q", got)
	}

	write(`{not json at all`)
	if got := BaseURL(); got != "https://brewkeg.dev" {
		t.Errorf("a malformed file was honoured: %q", got)
	}
}

// Enabled with no URL is the default gateway wearing a badge that says
// otherwise. Treating it as off keeps the banner honest.
func TestEnabledWithoutAURLIsNotDeveloperMode(t *testing.T) {
	withTempHome(t)
	t.Setenv("BREWKEG_BASE_URL", "")

	if err := os.MkdirAll(brewkegHome(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(DevSettingsPath(), []byte(`{"enabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if DevModeOn() {
		t.Error("developer mode claims to be on with nothing to point at")
	}
}

// The file sits next to the key store and names an internal host, so it must
// not be world-readable.
func TestDevSettingsFileIsOwnerOnly(t *testing.T) {
	withTempHome(t)
	requirePOSIXPerms(t)

	if err := SetDevBaseURL("http://localhost:3000"); err != nil {
		t.Fatalf("set: %v", err)
	}
	fi, err := os.Stat(DevSettingsPath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600", perm)
	}
}

// The whole point of the setting is that the URL written into a tool's config
// is the one in force, not a stale copy from when the window opened.
func TestApplyWritesTheOverrideURL(t *testing.T) {
	withTempHome(t)
	t.Setenv("BREWKEG_BASE_URL", "")

	if err := SetDevBaseURL("http://localhost:3456"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, _, err := Apply([]string{"codex"}, Options{APIKey: "bk_live_dev_0001"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got := ReadFile(HomeJoin(".codex", "config.toml"))
	if !contains(got, "http://localhost:3456/v1") {
		t.Fatalf("config.toml did not get the override URL:\n%s", got)
	}
	if contains(got, "brewkeg.dev") {
		t.Fatalf("config.toml still points at production:\n%s", got)
	}
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(hay); i++ {
			if hay[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
