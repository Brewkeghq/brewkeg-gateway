package brewkeg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// A Spec is what the server tells the app to write for each tool. Everything
// here is data — paths, key names, model ids, labels — so when Codex or Claude
// Code changes the shape of its config we push a new spec and every installed
// app follows, with no new release.
//
// What is deliberately NOT in the spec is the logic. The engine owns how to
// edit a TOML table in place, how to merge JSON, how to mark a block, how to
// back up and restore. Remote config describes intent; the engine decides how
// to carry it out safely. That is what lets this be data instead of code.
type Spec struct {
	Version int          `json:"version"`
	Targets []TargetSpec `json:"targets"`
	// Pickers replaces the model picker of a tool. Kept beside the targets
	// rather than inside them because a picker is a catalogue, not a config
	// edit: it lists every model brewkeg serves, which changes far more often
	// than the shape of any tool's config file.
	Pickers map[string]ModelPickerSpec `json:"pickers,omitempty"`
}

type TargetSpec struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Icon is an asset key the app ships ("anthropic", "openai", "brewkeg").
	Icon string `json:"icon,omitempty"`
	// Note is shown when the tool cannot be detected, e.g. "set in the app".
	Note string `json:"note,omitempty"`

	Detect DetectSpec `json:"detect"`
	// Enabled says how to tell whether this tool is already pointed at brewkeg.
	// It reads the config itself, never our own marker comments, so a user who
	// wired brewkeg in by hand still sees the right switch state.
	Enabled EnabledSpec `json:"enabled"`
	// Files are what we write. An empty Files with Manual set means we print
	// instructions instead, because the setting lives in a GUI we do not own.
	Files  []FileSpec `json:"files,omitempty"`
	Manual string     `json:"manual,omitempty"`
}

type DetectSpec struct {
	// Dirs and Files exist relative to $HOME; Bins are looked up on PATH and in
	// the usual install dirs (a Finder-launched app inherits a minimal PATH).
	Dirs  []string `json:"dirs,omitempty"`
	Files []string `json:"files,omitempty"`
	Bins  []string `json:"bins,omitempty"`
}

type EnabledSpec struct {
	// JSONEnvKeys: the tool is on when settings.json's "env" has any of these.
	JSONEnvKeys []string `json:"jsonEnvKeys,omitempty"`
	// ShellBlock: the tool is on when a shell rc carries our marked block.
	ShellBlock bool `json:"shellBlock,omitempty"`
	// TomlTable + RootKey/RootValue: the tool is on when that table exists and
	// the root-scope key selects it.
	TomlTable string `json:"tomlTable,omitempty"`
	RootKey   string `json:"rootKey,omitempty"`
	RootValue string `json:"rootValue,omitempty"`
	// NeverDetectable is for GUI-only tools whose config we cannot read.
	NeverDetectable bool `json:"neverDetectable,omitempty"`
}

type FileSpec struct {
	// Path is relative to $HOME, or absolute.
	Path string `json:"path"`
	// Kind selects the safe editor the engine uses:
	//   json-env     — merge into the "env" object of a JSON file
	//   shell-block  — a marked block of exports in an rc file
	//   toml-provider— a marked block, or an in-place edit of the table
	Kind string `json:"kind"`
	// Entries are the individual key/value pairs to write. One shape for all
	// three kinds: an env var name in json-env and shell-block, a TOML key in
	// toml-provider. Value may use the placeholders below.
	Entries []KVSpec `json:"entries,omitempty"`
	// Optional says an unknown shell means "skip this file", not "fail".
	Optional bool `json:"optional,omitempty"`
}

// KVSpec is one key brewkeg owns in a target's config.
type KVSpec struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Placeholders available inside a spec value.
const (
	phBaseURL   = "{{baseUrl}}"
	phBaseURLV1 = "{{baseUrlV1}}"
	phAPIKey    = "{{apiKey}}"
	phMainModel = "{{mainModel}}"
	phFastModel = "{{fastModel}}"
)

func expand(v string, o Options) string {
	return strings.NewReplacer(
		phBaseURL, o.BaseURL,
		phBaseURLV1, strings.TrimRight(o.BaseURL, "/")+"/v1",
		phAPIKey, o.APIKey,
		phMainModel, o.MainModel,
		phFastModel, o.FastModel,
	).Replace(v)
}

// DefaultSpec is what the app knows without the network. It matches what the
// server currently serves, so an offline launch behaves identically.
func DefaultSpec() Spec {
	return Spec{
		Version: 1,
		Targets: []TargetSpec{
			{
				ID: "claude-cli", Label: "Claude Code", Icon: "anthropic",
				Detect: DetectSpec{Dirs: []string{".claude"}, Bins: []string{"claude"}},
				Enabled: EnabledSpec{
					JSONEnvKeys: []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL", "ANTHROPIC_SMALL_FAST_MODEL"},
					ShellBlock:  true,
				},
				Files: []FileSpec{
					{Path: ".claude/settings.json", Kind: "json-env", Entries: claudeEnvEntries()},
					{Path: "", Kind: "shell-block", Entries: claudeEnvEntries(), Optional: true},
				},
			},
			{
				ID: "codex", Label: "Codex CLI", Icon: "openai",
				Detect: DetectSpec{Dirs: []string{".codex"}, Bins: []string{"codex"}},
				Enabled: EnabledSpec{
					TomlTable: "model_providers.brewkeg",
					RootKey:   "model_provider", RootValue: "brewkeg",
				},
				Files: []FileSpec{
					{Path: ".codex/config.toml", Kind: "toml-provider", Entries: []KVSpec{
						{Name: "name", Value: `"brewkeg"`},
						{Name: "base_url", Value: `"{{baseUrlV1}}"`},
						{Name: "wire_api", Value: `"responses"`},
						{Name: "experimental_bearer_token", Value: `"{{apiKey}}"`},
					}},
				},
			},
			{
				ID: "desktop", Label: "Claude Desktop", Icon: "anthropic",
				Detect:  DetectSpec{Files: []string{"Library/Application Support/Claude/config.json"}},
				Enabled: EnabledSpec{NeverDetectable: true},
				Note:    "set in the app",
				Manual:  "Open Claude Desktop\nDeveloper menu -> Configure Third-Party Inference...\nBase URL: {{baseUrl}}\nGateway API key: {{apiKey}}\nApply Changes, then fully quit and reopen the app",
			},
		},
	}
}

// SpecEndpoint is where the app asks for the current spec.
func SpecEndpoint(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/api/client-config"
}

// FetchSpec pulls the spec from the gateway. A failure is not an error the user
// should see: we fall back to the built-in spec so setup still works offline.
func FetchSpec(ctx context.Context, baseURL string) (Spec, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, SpecEndpoint(baseURL), nil)
	if err != nil {
		return DefaultSpec(), err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "brewkeg-client/"+Version)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return DefaultSpec(), err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return DefaultSpec(), fmt.Errorf("spec endpoint returned %d", res.StatusCode)
	}

	var spec Spec
	if err := json.NewDecoder(res.Body).Decode(&spec); err != nil {
		return DefaultSpec(), err
	}
	if len(spec.Targets) == 0 {
		return DefaultSpec(), fmt.Errorf("spec has no targets")
	}
	// A spec older than what we ship would undo a fix the app already knows
	// how to make, so never downgrade.
	if spec.Version < DefaultSpec().Version {
		return DefaultSpec(), fmt.Errorf("server spec v%d is older than the built-in v%d", spec.Version, DefaultSpec().Version)
	}
	return spec, nil
}

// claudeEnvEntries are the four variables that point Claude Code at brewkeg.
func claudeEnvEntries() []KVSpec {
	return []KVSpec{
		{Name: "ANTHROPIC_BASE_URL", Value: `"{{baseUrl}}"`},
		{Name: "ANTHROPIC_AUTH_TOKEN", Value: `"{{apiKey}}"`},
		{Name: "ANTHROPIC_MODEL", Value: `"{{mainModel}}"`},
		{Name: "ANTHROPIC_SMALL_FAST_MODEL", Value: `"{{fastModel}}"`},
	}
}

// resolvePath expands a spec path against $HOME.
func resolvePath(p string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~/") {
		return HomeJoin(strings.TrimPrefix(p, "~/"))
	}
	if strings.HasPrefix(p, "/") {
		return p
	}
	return filepath.Join(Home(), p)
}
