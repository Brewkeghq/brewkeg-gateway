package brewkeg

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
)

// Claude Desktop is not as unwritable as it looks.
//
// Third-party inference is configured in a plain JSON directory next to the
// app, not in a store we cannot read: `configLibrary/` holds one <id>.json per
// saved configuration plus a `_meta.json` naming the one that is applied. The
// in-app window writes these files; so can we.
//
// Everything below is based on the documented layout and on the files actually
// present on a real machine — not on the marker comments, and not on a guess.
// Where we could not verify a path, say so rather than inventing one.

// ClaudeDesktopSupportDir is the per-user Claude Desktop directory, which is
// different on every OS and carries a -3p suffix once the app runs in
// third-party mode.
//
// Windows is %LOCALAPPDATA% (AppData\Local), NOT %APPDATA% (Roaming). That is
// an easy and expensive mistake: Electron's *default* userData is Roaming, and
// the two only differ for this particular config directory.
func ClaudeDesktopSupportDir() string {
	switch runtime.GOOS {
	case "windows":
		return HomeJoin("AppData", "Local", "Claude-3p")
	case "linux":
		return HomeJoin(".config", "Claude-3p")
	default:
		return HomeJoin("Library", "Application Support", "Claude-3p")
	}
}

// ClaudeDesktopConfigDir is where the saved inference configurations live.
func ClaudeDesktopConfigDir() string {
	return filepath.Join(ClaudeDesktopSupportDir(), "configLibrary")
}

// ClaudeDesktopAppliedConfig returns the <id>.json of the configuration Claude
// Desktop is actually using, read from _meta.json. Empty when there is none —
// a machine that has never opened the third-party panel.
//
// Editing the applied file in place, rather than adding a new one, is the only
// safe move: writing a fresh <id>.json means also writing _meta.json to point
// at it, and guessing either shape is how you end up with an app pointed at a
// configuration the user never chose.
func ClaudeDesktopAppliedConfig() string {
	meta := ReadFile(filepath.Join(ClaudeDesktopConfigDir(), "_meta.json"))
	if strings.TrimSpace(meta) == "" {
		return ""
	}
	var m struct {
		AppliedID string `json:"appliedId"`
	}
	if json.Unmarshal([]byte(meta), &m) != nil || m.AppliedID == "" {
		return ""
	}
	// An id from a file we do not control must not escape the directory.
	if strings.ContainsAny(m.AppliedID, `/\`) || strings.Contains(m.AppliedID, "..") {
		return ""
	}
	p := filepath.Join(ClaudeDesktopConfigDir(), m.AppliedID+".json")
	if !FileExists(p) {
		return ""
	}
	return p
}
