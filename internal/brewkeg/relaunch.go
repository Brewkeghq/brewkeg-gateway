package brewkeg

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Restarting a tool is not cosmetic. A config file is read once, at launch, so
// writing it changes nothing until the app comes back. Telling the user to quit
// and reopen by hand works exactly once — the second time they forget, and the
// app looks broken while being perfectly configured.
//
// So we do it. The rule that keeps this from being destructive: only restart an
// app that was already running. Launching something the user had deliberately
// closed is not a restart, and it is not ours to decide.

// RelaunchApp names one desktop app we may cycle.
type RelaunchApp struct {
	// BundleID is the stable identifier (macOS) and is what we match on,
	// because an app's name on disk and its display name disagree often enough
	// that matching on either one alone eventually restarts the wrong thing.
	BundleID string `json:"bundleId,omitempty"`
	// Name is the human label shown in the window, e.g. "Claude Desktop".
	Name string `json:"name"`
	// Bins are process-name candidates used on Windows and Linux, where there
	// is no bundle id. First match wins.
	Bins []string `json:"bins,omitempty"`
}

// RelaunchResult is one line in the report the window shows.
type RelaunchResult struct {
	App    string `json:"app"`
	Action string `json:"action"` // restarted | not running | could not restart
	Detail string `json:"detail,omitempty"`
}

// Relaunch quits and reopens each app that is currently running.
//
// quitWait is how long we let an app exit gracefully before giving up. Force
// killing is not on the table: an editor or an agent session with unsaved state
// is worth more than a faster restart, so a slow quit is reported rather than
// escalated.
func Relaunch(apps []RelaunchApp, quitWait time.Duration) []RelaunchResult {
	// A test run must not quit the developer's Claude Desktop. The suite calls
	// the same Configure the window does, so the guard belongs here rather than
	// at the call site, where the next caller would forget it.
	if os.Getenv("BREWKEG_NO_RELAUNCH") != "" {
		return nil
	}
	out := make([]RelaunchResult, 0, len(apps))
	for _, a := range apps {
		out = append(out, relaunchOne(a, quitWait))
	}
	return out
}

func relaunchOne(a RelaunchApp, quitWait time.Duration) RelaunchResult {
	res := RelaunchResult{App: a.Name}

	running, how := findProcess(a)
	if !running {
		// Not running: leave it closed. Starting it would be a surprise.
		res.Action = "not running"
		return res
	}
	res.Detail = how

	if err := quitProcess(a, how); err != nil {
		res.Action = "could not restart"
		res.Detail = err.Error()
		return res
	}
	if err := waitForExit(a, quitWait); err != nil {
		res.Action = "could not restart"
		res.Detail = "did not quit within " + quitWait.String() + " — left it alone"
		return res
	}
	if err := startProcess(a); err != nil {
		res.Action = "could not restart"
		res.Detail = "quit, but would not reopen: " + err.Error()
		return res
	}
	res.Action = "restarted"
	return res
}

/* ------------------------------------------------------------------ macOS */

func darwinRunning(a RelaunchApp) (bool, string) {
	// "application id X is running" resolves through LaunchServices, so a
	// renamed app on disk or a stale path still matches.
	out, err := exec.Command("osascript", "-e",
		`application id "`+a.BundleID+`" is running`).Output()
	if err != nil {
		return false, ""
	}
	if strings.TrimSpace(string(out)) != "true" {
		return false, ""
	}
	return true, "bundle " + a.BundleID
}

func darwinQuit(a RelaunchApp) error {
	return exec.Command("osascript", "-e",
		`tell application id "`+a.BundleID+`" to quit`).Run()
}

func darwinStart(a RelaunchApp) error {
	// `open` is the supported way to launch a bundle id. Using -a with a
	// display name would fail on a localized or renamed app.
	return exec.Command("open", "-b", a.BundleID).Run()
}

func darwinWait(bundleID string, d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		out, err := exec.Command("osascript", "-e",
			`application id "`+bundleID+`" is running`).Output()
		if err != nil || strings.TrimSpace(string(out)) != "true" {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errStillRunning
}

/* ------------------------------------------------- Windows and Linux */

// These are best-effort by necessity: neither platform has LaunchServices, so
// we match on process name and relaunch by whatever path we found. If we cannot
// find a way back in, we say so rather than starting a second copy.

func windowsRunning(a RelaunchApp) (bool, string) {
	for _, b := range a.Bins {
		if out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+b).Output(); err == nil &&
			strings.Contains(string(out), b) {
			return true, b
		}
	}
	return false, ""
}

func windowsQuit(a RelaunchApp, how string) error {
	return exec.Command("taskkill", "/IM", how, "/F").Run()
}

func windowsStart(a RelaunchApp) error {
	if len(a.Bins) == 0 {
		return errNoLauncher
	}
	cmd := exec.Command("cmd", "/c", "start", "", a.Bins[0])
	return cmd.Start()
}

func linuxRunning(a RelaunchApp) (bool, string) {
	for _, b := range a.Bins {
		if processAlive(b) {
			return true, b
		}
	}
	return false, ""
}

// processAlive asks the question; it must never signal the process. An earlier
// version called `pkill` here to find out whether something was running, which
// meant asking "are you open?" killed the app.
func processAlive(bin string) bool {
	out, err := exec.Command("pgrep", "-f", bin).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func linuxQuit(a RelaunchApp, how string) error {
	return exec.Command("pkill", "-TERM", "-f", how).Run()
}

func linuxStart(a RelaunchApp) error {
	if len(a.Bins) == 0 {
		return errNoLauncher
	}
	cmd := exec.Command("nohup", a.Bins[0])
	cmd.Stdout, cmd.Stderr = nil, nil
	return cmd.Start()
}

/* ------------------------------------------------------------------ glue */

var (
	errStillRunning = &appError{"it is still running"}
	errNoLauncher   = &appError{"no way to launch it again on this platform"}
)

type appError struct{ msg string }

func (e *appError) Error() string { return e.msg }

func findProcess(a RelaunchApp) (bool, string) {
	switch runtime.GOOS {
	case "darwin":
		return darwinRunning(a)
	case "windows":
		return windowsRunning(a)
	default:
		return linuxRunning(a)
	}
}

func quitProcess(a RelaunchApp, how string) error {
	switch runtime.GOOS {
	case "darwin":
		return darwinQuit(a)
	case "windows":
		return windowsQuit(a, how)
	default:
		return linuxQuit(a, how)
	}
}

func startProcess(a RelaunchApp) error {
	switch runtime.GOOS {
	case "darwin":
		return darwinStart(a)
	case "windows":
		return windowsStart(a)
	default:
		return linuxStart(a)
	}
}

// waitForExit gives the app its grace period to shut down cleanly.
//
// macOS is the only platform where we can *ask* whether a bundle id is gone
// rather than infer it from a process name, so only there do we poll; the rest
// wait out the same window. Force-killing is deliberately not an option: an
// agent session with unsaved state is worth more than a fast restart.
func waitForExit(a RelaunchApp, d time.Duration) error {
	if runtime.GOOS != "darwin" {
		time.Sleep(d)
		return nil
	}
	return darwinWait(a.BundleID, d)
}

// RestartAppsFor collects the desktop apps the given targets need cycled,
// de-duplicated: turning on Claude Code and Claude Desktop should never produce
// two attempts to restart the same app.
func RestartAppsFor(ids []string) []RelaunchApp {
	spec := DefaultSpec()
	seen := map[string]bool{}
	var out []RelaunchApp
	for _, id := range ids {
		for _, ts := range spec.Targets {
			if ts.ID != id {
				continue
			}
			for _, a := range ts.RestartApps {
				key := a.BundleID
				if key == "" {
					key = a.Name
				}
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, a)
			}
		}
	}
	return out
}
