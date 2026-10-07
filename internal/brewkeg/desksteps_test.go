package brewkeg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func devSettingsPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "Claude-3p", "developer_settings.json")
}

func writeDevSettings(t *testing.T, body string) {
	t.Helper()
	p := devSettingsPath(t)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDeveloperModeDetection(t *testing.T) {
	withTempHome(t)

	if ClaudeDesktopDeveloperModeOn() {
		t.Fatal("a machine with no developer_settings.json must not read as enabled")
	}

	// A quoted "true" is a string to Electron, not a boolean. Treating it as on
	// would hide the step the user actually still has to do.
	writeDevSettings(t, "{\n  \"allowDevTools\": \"true\"\n}\n")
	if ClaudeDesktopDeveloperModeOn() {
		t.Fatal(`a string "true" is not a boolean and must not count as enabled`)
	}

	writeDevSettings(t, "{\n  \"allowDevTools\": false\n}\n")
	if ClaudeDesktopDeveloperModeOn() {
		t.Fatal("allowDevTools:false must not count as enabled")
	}

	writeDevSettings(t, "{\n  \"allowDevTools\": true\n}\n")
	if !ClaudeDesktopDeveloperModeOn() {
		t.Fatal("allowDevTools:true must count as enabled")
	}
}

// The manual steps now exist only to explain the one thing we cannot do: turn
// on Developer Mode, which controls whether the menu is visible inside the app.
// They must never tell the user to sign in or to build a profile — we create
// the profile ourselves now.
func TestManualStepsNeverAskForSomethingWeDo(t *testing.T) {
	withTempHome(t)

	// Find it by id: the spec is a list and targets get added, so "last" is a
	// property of today's ordering rather than of the desktop target.
	spec := DefaultSpec()
	tgt := specTarget{}
	found := false
	for _, ts := range spec.Targets {
		if ts.ID == "desktop" {
			tgt = specTarget{spec: ts}
			found = true
		}
	}
	if !found {
		t.Fatal("no desktop target in the built-in spec")
	}

	// Developer Mode off, profile absent — the state a fresh install is in.
	got := tgt.ManualInstructions(Options{APIKey: "bk_live_K", BaseURL: "https://brewkeg.dev"})
	if !strings.Contains(got, "Enable Developer Mode") {
		t.Fatalf("the developer-mode step is missing:\n%s", got)
	}
	for _, forbidden := range []string{"Sign in", "sign in", "Configure Third-Party Inference"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("steps still tell the user to do something we now do ourselves (%q):\n%s", forbidden, got)
		}
	}

	// Developer Mode already on: nothing left to say.
	writeDevSettings(t, "{\n  \"allowDevTools\": true\n}\n")
	if got := tgt.ManualInstructions(Options{APIKey: "bk_live_K", BaseURL: "https://brewkeg.dev"}); got != "" {
		t.Fatalf("expected no manual steps with Developer Mode on, got:\n%s", got)
	}
}

// A condition this build does not understand must be skipped, not guessed at.
func TestUnknownManualConditionIsSkipped(t *testing.T) {
	if manualConditionHolds("some-future-condition") {
		t.Fatal("an unknown condition must not be treated as satisfied")
	}
	if !manualConditionHolds("") {
		t.Fatal("an empty condition means always")
	}
}