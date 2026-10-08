package brewkeg

import (
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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

// ClaudeDesktopDeveloperModeOn reports whether the Developer menu is already
// unlocked, by reading the one setting that flag writes.
//
// This is the difference between "nothing happened" and an explanation. Signing
// in to Claude Desktop does not create the -3p directory at all — only enabling
// Developer Mode does. So a user who has signed in, pressed Connect, and got a
// fixed script back has already done the first thing on the list and still
// cannot get to the Developer menu, because the toggle that reveals it is a
// separate step they have no reason to know about. Knowing the difference lets
// us print only the step that is actually outstanding.
//
// A false here means "assume it is off and show the step", never "skip it":
// printing one redundant line costs the user a glance, while wrongly skipping
// it sends them to a menu that is not there.
func ClaudeDesktopDeveloperModeOn() bool {
	raw := ReadFile(filepath.Join(ClaudeDesktopSupportDir(), "developer_settings.json"))
	if strings.TrimSpace(raw) == "" {
		return false
	}
	var doc struct {
		AllowDevTools *bool `json:"allowDevTools"`
	}
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return false
	}
	return doc.AllowDevTools != nil && *doc.AllowDevTools
}

// configID is the shape the app accepts for a configuration id. From the
// bundle: `var bBe = /^[a-f0-9-]{36}$/`, and every read path guards the applied
// id with it. An id that fails this test is not a path problem, it is a config
// the app will silently ignore — so we reject it here rather than writing a
// file that never gets read.
var configID = regexp.MustCompile(`^[a-f0-9-]{36}$`)

// ClaudeDesktopAppliedConfig returns the <id>.json of the configuration Claude
// Desktop is actually using, read from _meta.json. Empty when there is none.
//
// Editing the applied file in place is preferred over adding a new one: a
// machine that already has a profile has one the user chose, and replacing it
// would silently repoint an app they had configured on purpose.
func ClaudeDesktopAppliedConfig() string {
	meta := ReadFile(filepath.Join(ClaudeDesktopConfigDir(), "_meta.json"))
	if strings.TrimSpace(meta) == "" {
		return ""
	}
	var m struct {
		AppliedID string `json:"appliedId"`
	}
	if json.Unmarshal([]byte(meta), &m) != nil {
		return ""
	}
	// An id from a file we do not control must both match the shape the app
	// accepts and stay inside the directory.
	if !configID.MatchString(m.AppliedID) {
		return ""
	}
	p := filepath.Join(ClaudeDesktopConfigDir(), m.AppliedID+".json")
	if !FileExists(p) {
		return ""
	}
	return p
}

// ClaudeDesktopEnsureProfile returns the path of the applied <id>.json, creating
// the whole configuration library if it does not exist yet.
//
// This used to be the thing we refused to do, and the refusal was wrong. We had
// only read the *read* side of the format out of the bundle and assumed the
// write side had to match a GUI we could not see. It does not. The app bootstraps
// the directory itself on first access, and its own code is the spec:
//
//	let id = randomUUID();          // 36 chars, [a-f0-9-]
//	write <id>.json  = {}
//	write _meta.json = { appliedId: id, entries: [{ id, name: "Default" }] }
//
// Reproducing that is not a guess — it is the same three lines, and an id we
// mint passes the same guard the app applies to ids it reads. The result is that
// a user who has never opened the third-party panel is no longer told to go and
// open it: the profile simply exists when we get here.
//
// Entries are validated by the app as {id: string, name: string, provider?:
// string, note?: string}, and _meta.json is only re-validated on the *managed*
// path. We write the minimal shape the app itself writes and nothing more.
func ClaudeDesktopEnsureProfile() (string, error) {
	if p := ClaudeDesktopAppliedConfig(); p != "" {
		return p, nil
	}
	dir := ClaudeDesktopConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	id, err := newConfigID()
	if err != nil {
		return "", err
	}
	entry := filepath.Join(dir, id+".json")
	if err := WriteFile(entry, "{}\n"); err != nil {
		return "", err
	}
	meta, err := json.MarshalIndent(struct {
		AppliedID string `json:"appliedId"`
		Entries   []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"entries"`
	}{AppliedID: id, Entries: []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}{{ID: id, Name: "Default"}}}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := WriteFile(filepath.Join(dir, "_meta.json"), string(meta)+"\n"); err != nil {
		return "", err
	}
	return entry, nil
}

// newConfigID mints an RFC 4122 v4 UUID in the lowercase dashed form the app
// accepts. crypto/rand rather than math/rand: a predictable id would let a
// second process predict which profile file we are about to write.
func newConfigID() (string, error) {
	var b [16]byte
	if _, err := crand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}
