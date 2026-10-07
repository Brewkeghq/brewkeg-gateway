package brewkeg

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ModelPickerSpec replaces the model picker of a tool outright. Claude Code
// honours `modelPicker.replaceBuiltInOptions`, which lets us show exactly the
// catalogue brewkeg actually serves — and nothing else — without shipping a
// new app when the catalogue changes.
type ModelPickerSpec struct {
	// Path is where the picker block lives, relative to $HOME.
	Path string `json:"path"`
	// Replace drops the tool's own options and shows only ours.
	Replace bool           `json:"replace"`
	Options []PickerOption `json:"options"`
}

type PickerOption struct {
	Model       string `json:"model"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

// Pickers maps target id to the picker it should write.
type Pickers map[string]ModelPickerSpec

// applyPicker writes the picker block into the target's settings file. It is a
// merge, like every other write we do: the file also holds env, hooks and
// permissions, and none of those may move.
func applyPicker(path string, p ModelPickerSpec) error {
	if len(p.Options) == 0 {
		return nil
	}
	doc := map[string]any{}
	if raw := ReadFile(path); strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			return fmt.Errorf("%s is not valid JSON — fix or move it, then re-run", path)
		}
	}

	opts := make([]any, 0, len(p.Options))
	for _, o := range p.Options {
		entry := map[string]any{"model": o.Model}
		if o.Label != "" {
			entry["label"] = o.Label
		}
		if o.Description != "" {
			entry["description"] = o.Description
		}
		opts = append(opts, entry)
	}
	doc["modelPicker"] = map[string]any{
		"replaceBuiltInOptions": p.Replace,
		"options":               opts,
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(path, string(out)+"\n")
}
