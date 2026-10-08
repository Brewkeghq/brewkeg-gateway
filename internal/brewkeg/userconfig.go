package brewkeg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// UserConfigPath is ~/.brewkeg/config.json.
//
// It exists so nobody has to click through menus: a scriptable answer to
// "point this machine at brewkeg". Both products read it — the app on launch,
// the CLI with --yes — and every field is optional, so a file naming only the
// key is a perfectly good file.
func UserConfigPath() string { return filepath.Join(brewkegHome(), "config.json") }

// UserConfig is what a person wrote down once, by hand or by a provisioning
// script, so later runs need no input.
type UserConfig struct {
	APIKey  string   `json:"apiKey,omitempty"`
	Targets []string `json:"targets,omitempty"`
	BaseURL string   `json:"baseUrl,omitempty"`
	Model   string   `json:"model,omitempty"`
	// SonnetModel is the middle of the lineup. Optional: when it is absent,
	// the engine fills in the default rather than dropping the family.
	SonnetModel string `json:"sonnetModel,omitempty"`
	FastModel   string `json:"fastModel,omitempty"`
	CodexModel  string `json:"codexModel,omitempty"`
}

// LoadUserConfig reads the config file. A missing or unreadable file is not an
// error — it just means the user has not written one.
func LoadUserConfig() UserConfig {
	var c UserConfig
	raw := ReadFile(UserConfigPath())
	if strings.TrimSpace(raw) == "" {
		return c
	}
	if json.Unmarshal([]byte(raw), &c) != nil {
		// A hand-edited file with a typo must not be silently half-read.
		return UserConfig{}
	}
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	return c
}

// SaveUserConfig writes the config file 0600: it usually holds the key.
func SaveUserConfig(c UserConfig) error {
	if err := EnsurePrivateDir(brewkegHome()); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(UserConfigPath(), append(raw, '\n'), 0o600)
}

// Resolve merges the config file with explicit overrides. The file is the
// base; anything passed on the command line or typed into the window wins.
// An override only replaces what it actually names, so `--api-key` alone does
// not erase the target list from the file.
func (c UserConfig) Resolve(o Options) Options {
	out := o
	if out.APIKey == "" {
		out.APIKey = c.APIKey
	}
	if out.BaseURL == "" {
		out.BaseURL = c.BaseURL
	}
	if out.MainModel == "" {
		out.MainModel = c.Model
	}
	if out.SonnetModel == "" {
		out.SonnetModel = c.SonnetModel
	}
	if out.FastModel == "" {
		out.FastModel = c.FastModel
	}
	if out.CodexModel == "" {
		out.CodexModel = c.CodexModel
	}
	return out.WithDefaults()
}

// ConfiguredTargets returns the target ids named in the file, filtered against
// what the spec actually knows. An unknown id is dropped rather than passed
// through: a config written for an older release must not fail a newer one.
func ConfiguredTargets(s Spec) []string {
	want := loadRawTargets()
	out := make([]string, 0, len(want))
	for _, id := range want {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := TargetByIDFrom(s, id); !ok {
			continue
		}
		out = append(out, id)
	}
	return out
}

func loadRawTargets() []string { return LoadUserConfig().Targets }

// LabelFor is the spec's name for a target id, for --yes output. Unknown ids
// are filtered out by ConfiguredTargets before this is reached; the fallback
// keeps it total anyway.
func LabelFor(s Spec, id string) string {
	if t, ok := TargetByIDFrom(s, id); ok {
		return t.Label()
	}
	return id
}
