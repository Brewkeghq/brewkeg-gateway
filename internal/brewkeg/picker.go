package brewkeg

import (
	"encoding/json"
	"strings"
)

// The Claude Code model picker is the user's, not ours.
//
// brewkeg used to write `modelPicker.replaceBuiltInOptions` into
// ~/.claude/settings.json to swap Claude Code's own rows for brewkeg's
// catalogue. That is a rewrite of a picker the user did not ask us to touch:
// it removes the models they already had, it reappears on every connect, and
// a tool that gains or renames a row shows up here as a config we overwrote.
// So we no longer write one — not from the server spec, not from the built-in
// default, not from `brewkeg launch`.
//
// What remains is cleanup. An install from before this change can still carry
// a block we wrote, and leaving it behind means the picker keeps advertising a
// gateway that has just been disconnected. PickerInstalled is the marker that
// separates that block from one the user wrote themselves, and it is the only
// reason this file still exists.

// claudeCLISettingsPath is the one file brewkeg ever wrote a picker into,
// relative to $HOME. Cleanup targets it directly rather than reading a path out
// of the spec: the spec no longer carries one, and an install that needs
// cleaning is by definition reading a spec that still did.
const claudeCLISettingsPath = ".claude/settings.json"

// PickerInstalled reports whether a file carries a modelPicker block brewkeg
// wrote. Used as an "is this tool configured" signal and as the guard on
// cleanup, so it has to be precise: a user who wrote their own modelPicker is
// NOT configured by us and must not have it deleted.
//
// The label is the marker. Every option brewkeg installed is labelled
// "…· brewkeg", and we wrote the block as a whole — so a single matching
// option means the block is ours.
func PickerInstalled(path string) bool {
	doc := map[string]any{}
	if err := json.Unmarshal([]byte(ReadFile(path)), &doc); err != nil {
		return false
	}
	picker, _ := doc["modelPicker"].(map[string]any)
	if picker == nil {
		return false
	}
	for _, o := range asSlice(picker["options"]) {
		m, _ := o.(map[string]any)
		if label, _ := m["label"].(string); strings.Contains(label, "brewkeg") {
			return true
		}
	}
	return false
}
