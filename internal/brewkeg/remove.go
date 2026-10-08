package brewkeg

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Eviction is the other half of the switch. Turning a tool *on* merges
// brewkeg's keys into its config; turning it *off* has to take them back out.
// Stopping writes is not the same as disconnecting: a user who untoggles Claude
// Code and presses the button expects Claude Code to stop talking to brewkeg,
// not to keep an ANTHROPIC_BASE_URL that silently redirects every request
// through a gateway they just switched off.
//
// Every remover here is the exact inverse of its writer, and every one of them
// is surgical in the same way: only keys brewkeg owns are touched, and the
// user's own settings in the same file are left byte-for-byte alone. The file
// has already been captured in the backup before any of them runs.

// removeJSONKeys deletes brewkeg's keys from a JSON document and reports whether
// anything was actually there to remove. nested selects the "env" object rather
// than the top level.
//
// A key is removed by name. The spec is the list of names brewkeg owns, so a
// value the user edited after we wrote it is still brewkeg's slot — leaving it
// behind would mean the tool keeps pointing at a gateway the user turned off.
func removeJSONKeys(path string, entries []KVSpec, nested bool) (bool, error) {
	raw := ReadFile(path)
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}
	doc := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return false, fmt.Errorf("%s is not valid JSON — fix or move it, then re-run", path)
	}

	container := doc
	if nested {
		var ok bool
		container, ok = doc["env"].(map[string]any)
		if !ok {
			return false, nil
		}
	}

	removed := false
	for _, e := range entries {
		if _, ok := container[e.Name]; ok {
			delete(container, e.Name)
			removed = true
		}
	}
	if !removed {
		return false, nil
	}
	// An env object we emptied out is our own residue. Leaving `"env": {}`
	// behind adds noise to every future diff of the file.
	if nested && len(container) == 0 {
		delete(doc, "env")
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	return true, WriteFile(path, string(out)+"\n")
}

// removeShellBlock strips brewkeg's marked export block from a shell rc.
func removeShellBlock(path string) (bool, error) {
	current := ReadFile(path)
	if !HasBlock(current) {
		return false, nil
	}
	next, _ := StripBlock(current)
	return true, WriteFile(path, next)
}

// removeTOMLProvider takes brewkeg back out of a config.toml: our marked block,
// the [model_providers.brewkeg] table, and the root key that selected it — plus
// the foreign value we commented out on the way in, which is put back.
func removeTOMLProvider(path string, roots []KVSpec) (bool, error) {
	current := ReadFile(path)
	if strings.TrimSpace(current) == "" {
		return false, nil
	}

	next, hadBlock := StripBlock(current)
	next, hadTable := removeTable(next, "model_providers.brewkeg")
	for _, r := range roots {
		if r.Name == "" {
			continue
		}
		var hadRoot bool
		next, hadRoot = restoreRootKey(next, r.Name)
		hadBlock = hadBlock || hadRoot
	}
	if !hadBlock && !hadTable {
		return false, nil
	}
	return true, WriteFile(path, next)
}

// removeTable deletes a [table] and every line belonging to it, up to the next
// table header or the end of the file.
func removeTable(content, table string) (string, bool) {
	lines := strings.Split(content, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "["+table+"]" {
			start = i
			break
		}
	}
	if start < 0 {
		return content, false
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}
	merged := append([]string{}, lines[:start]...)
	merged = append(merged, lines[end:]...)
	return strings.Join(merged, "\n"), true
}

// restoreRootKey puts back the value we commented out when we took over a
// root-scope key, in the position the comment occupied, and drops the comment.
//
// In place matters. The comment is a one-for-one stand-in for the user's own
// line, so lifting the value back out and re-appending it near the first table
// header silently moves it — and a config.toml that has been reflowed around a
// key the user wrote is a config they did not agree to.
//
// The "only when nothing is live" guard is the other half. Restoring into a
// file that already has a live root assignment would be a duplicate-key parse
// error, and a config.toml that will not parse is a Codex the user cannot
// start. Only the root region counts: the same key inside a table belongs to
// that table and is none of our business.
func restoreRootKey(content, key string) (string, bool) {
	lines := strings.Split(content, "\n")

	// Root scope is everything before the first table header.
	rootEnd := len(lines)
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "[") {
			rootEnd = i
			break
		}
	}
	live := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)
	hasLive := false
	for i := 0; i < rootEnd; i++ {
		t := strings.TrimSpace(lines[i])
		if live.MatchString(lines[i]) && !strings.HasPrefix(t, "#") {
			hasLive = true
			break
		}
	}
	if hasLive {
		return content, false
	}

	var restored string
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, replacedPrefix) {
			if k, v, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(t, replacedPrefix)), "="); ok &&
				strings.TrimSpace(k) == key {
				if restored == "" {
					restored = strings.TrimSpace(k) + " = " + strings.TrimSpace(v)
					out = append(out, restored)
				}
				continue
			}
		}
		out = append(out, l)
	}
	if restored == "" {
		return content, false
	}
	return strings.Join(out, "\n"), true
}

// removePickerKey drops the modelPicker block brewkeg installed wholesale. It is
// our own object — the user never had one — so removing it whole is right, and
// leaving it would keep advertising models from a gateway that is switched off.
func removePickerKey(path string) (bool, error) {
	raw := ReadFile(path)
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}
	doc := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return false, fmt.Errorf("%s is not valid JSON — fix or move it, then re-run", path)
	}
	if _, ok := doc["modelPicker"]; !ok {
		return false, nil
	}
	delete(doc, "modelPicker")
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	return true, WriteFile(path, string(out)+"\n")
}

// removeTarget is the inverse of applyTarget: same file discovery, same
// backup-before-write ordering, one remover per file kind. It reports the files
// it changed so the docket can show a removal, not just an addition.
func removeTarget(t Target, s Spec, b *Backup) (string, error) {
	st, ok := t.(specTarget)
	if !ok {
		return "", nil
	}

	var touched []string
	for _, f := range st.spec.Files {
		path := ""
		if f.PathForOS() != "" {
			path = resolvePath(f.PathForOS())
		} else if f.Kind == "shell-block" {
			path = ShellRC()
			if path == "" {
				continue
			}
		}
		if err := b.Capture(t.ID(), path); err != nil {
			return "", err
		}
		if f.Kind == "desktop-3p" {
			path = ClaudeDesktopAppliedConfig()
			if path == "" {
				// Nothing of ours is in it. Do NOT create a profile here to then
				// strip the keys back out of — that would leave a library the
				// user never asked for.
				continue
			}
		}

		var changed bool
		var err error
		switch f.Kind {
		case "json-env":
			changed, err = removeJSONKeys(path, f.Entries, true)
		case "json-plain", "desktop-3p":
			changed, err = removeJSONKeys(path, f.Entries, false)
		case "shell-block":
			changed, err = removeShellBlock(path)
		case "toml-provider":
			changed, err = removeTOMLProvider(path, rootKeysOf(st.spec.Enabled))
		case "zcode-provider":
			changed, err = RemoveZCodeProvider(path)
		default:
			return "", fmt.Errorf("unknown file kind %q in spec", f.Kind)
		}
		if err != nil {
			return "", err
		}
		if changed {
			touched = append(touched, path)
		}
	}

	if p, ok := s.Pickers[t.ID()]; ok {
		pp := resolvePath(p.Path)
		if err := b.Capture(t.ID(), pp); err == nil {
			if changed, err := removePickerKey(pp); err != nil {
				return "", err
			} else if changed {
				touched = append(touched, pp)
			}
		}
	}

	if len(touched) == 0 {
		return "", nil
	}
	return strings.Join(touched, " + "), nil
}

// AnyEnabled reports whether brewkeg is currently in any tool's config. The
// desktop app uses it to tell "disconnect everything" apart from "there is
// nothing connected to disconnect", which is the difference between an empty
// selection being a valid action and a dead button.
func AnyEnabled() bool { return anyEnabledIn(DefaultSpec()) }

func anyEnabledIn(s Spec) bool {
	for _, ts := range s.Targets {
		if enabledFor(specTarget{spec: ts}, s) {
			return true
		}
	}
	return false
}

// evictTargets removes brewkeg from every target that is currently configured
// but was not in the wanted set.
//
// The "currently configured" test is what keeps this safe: a target the user
// never turned on has nothing of ours in it, and a run that only ever touches
// codex must not go looking in ~/.claude. It is the same enabledFor() the
// switch state in the UI is drawn from, so the button and the action can never
// disagree about what is on.
func evictTargets(s Spec, wanted map[string]bool, b *Backup) []ApplyResult {
	var out []ApplyResult
	for _, ts := range s.Targets {
		if wanted[ts.ID] {
			continue
		}
		t := specTarget{spec: ts}
		if !enabledFor(t, s) {
			continue
		}
		r := ApplyResult{ID: ts.ID, Label: ts.Label}
		path, err := removeTarget(t, s, b)
		if err != nil {
			r.Error = err.Error()
			out = append(out, r)
			continue
		}
		r.OK = true
		r.Removed = true
		if path != "" {
			r.Paths = strings.Split(path, " + ")
		}
		out = append(out, r)
	}
	return out
}
