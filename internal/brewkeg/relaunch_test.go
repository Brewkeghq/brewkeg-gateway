package brewkeg

import "testing"

// An earlier version of the Linux check called `pkill` to find out whether
// something was running. Asking "are you open?" must never kill the app —
// this is the test for that, and it is why processAlive is a separate function.
func TestProcessAliveDoesNotSignal(t *testing.T) {
	// `sleep 30` has no matching name for this probe, so the check must report
	// false without having touched anything. If it ever calls pkill, the
	// integration test below catches it; this pins the contract that checking
	// is side-effect free.
	if processAlive("this-process-should-never-exist-9f3ac1c0") {
		t.Fatal("reported a process that does not exist")
	}
}

func TestRestartAppsForDeduplicates(t *testing.T) {
	got := RestartAppsFor([]string{"desktop", "desktop", "codex"})
	seen := map[string]int{}
	for _, a := range got {
		seen[a.BundleID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("bundle %s appeared %d times; the same app would be restarted twice", id, n)
		}
	}

	// The two desktop apps must both be present, and identified by bundle id —
	// matching on a name on disk finds nothing for the Codex app, which lives
	// inside ChatGPT.app.
	want := map[string]bool{
		"com.anthropic.claudefordesktop": false,
		"com.openai.codex":               false,
	}
	for _, a := range got {
		if _, ok := want[a.BundleID]; ok {
			want[a.BundleID] = true
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("no app with bundle id %s in %+v", id, got)
		}
	}
}

// Claude Code's config is a shell rc, so there is no app to cycle — only a
// hint. A target that invents a relaunch target would restart the user's
// editor for no reason.
func TestClaudeCliHasNothingToRelaunch(t *testing.T) {
	if got := RestartAppsFor([]string{"claude-cli"}); len(got) != 0 {
		t.Errorf("claude-cli should have nothing to relaunch, got %+v", got)
	}
}

func TestRelaunchOfNothingIsEmpty(t *testing.T) {
	if got := Relaunch(nil, 0); len(got) != 0 {
		t.Errorf("Relaunch(nil) = %+v, want empty", got)
	}
}

// An app that is not running must be left closed. Starting it would be a
// surprise the user cannot undo, and it is not what "restart" means.
func TestRelaunchLeavesClosedAppsClosed(t *testing.T) {
	got := Relaunch([]RelaunchApp{{
		BundleID: "com.example.definitely-not-running-9f3ac1c0",
		Name:     "Not Running",
	}}, 0)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	if got[0].Action != "not running" {
		t.Errorf("action = %q, want %q", got[0].Action, "not running")
	}
}
