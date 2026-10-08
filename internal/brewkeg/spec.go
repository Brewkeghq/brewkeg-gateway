package brewkeg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
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

// ManualStep is a block of instructions that only applies in some states.
//
// A fixed script re-tells the user to do things they already did, and the step
// they *cannot* do — the Developer menu that only exists once Developer Mode is
// on — gets buried in the middle of a list that looks already-finished. The
// condition lets the server decide which lines apply; the engine only knows how
// to answer the question.
type ManualStep struct {
	// When is a condition id. Empty means always. Unknown conditions are treated
	// as "do not apply", so a spec written for a newer app never has a step
	// skipped because an older engine cannot evaluate it.
	When  string   `json:"when,omitempty"`
	Lines []string `json:"lines"`
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
	// ManualSteps is the conditional form of Manual. When present it wins, and
	// only the blocks whose condition holds are printed.
	ManualSteps []ManualStep `json:"manualSteps,omitempty"`
	// RestartApps are the desktop apps that must be cycled for this target's
	// change to take effect. A config file is read once at launch, so writing
	// it does nothing until the app comes back.
	RestartApps []RelaunchApp `json:"restartApps,omitempty"`
}

type DetectSpec struct {
	// Dirs and Files exist relative to $HOME; Bins are looked up on PATH and in
	// the usual install dirs (a Finder-launched app inherits a minimal PATH).
	Dirs  []string `json:"dirs,omitempty"`
	Files []string `json:"files,omitempty"`
	Bins  []string `json:"bins,omitempty"`
	// FilesByOS is the same idea as FileSpec.Paths: an app whose config lives
	// somewhere different on each OS needs one path per OS, or it looks missing
	// on two platforms out of three. "os" is the fallback for anything unnamed.
	FilesByOS map[string]string `json:"filesByOS,omitempty"`
	// DirsByOS is FilesByOS for directories. Needed because checking a
	// directory with a file test always fails: Claude Desktop's detect entry
	// named its support directory, which is not a file, so the tool reported
	// "not detected" on a machine where it was plainly installed.
	DirsByOS map[string][]string `json:"dirsByOS,omitempty"`
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
	// RootKeys are further root-scope TOML keys this tool needs, written and
	// evicted by the same rules as RootKey: a pre-existing value is commented
	// out as `# brewkeg replaced:`, never deleted.
	//
	// Codex is why this is a list. Pointing it at brewkeg is not only
	// `model_provider = "brewkeg"` — Codex also has to be told WHICH model to
	// run, and its own default (`gpt-5.2`) is not a model brewkeg serves, so
	// every prompt 400s with "model_not_supported". One root key could not fix
	// both.
	RootKeys []KVSpec `json:"rootKeys,omitempty"`
	// JSONFileKeys: the tool is on when a top-level JSON config has any of
	// these keys with a non-empty value. Used where the config is not an env
	// bag, e.g. Claude Desktop's inference settings.
	JSONFileKeys []string `json:"jsonFileKeys,omitempty"`
	// ZCodeProvider is the path of ZCode's provider registry. The tool is on
	// when that registry names our providerId. A dedicated field because the
	// registry is two nested arrays of rule objects — no flat key scan finds it.
	ZCodeProvider string `json:"zcodeProvider,omitempty"`
	// NeverDetectable is for GUI-only tools whose config we cannot read.
	NeverDetectable bool `json:"neverDetectable,omitempty"`
}

type FileSpec struct {
	// Path is relative to $HOME, or absolute. It is the default; use Paths when
	// the same setting lives somewhere different per OS.
	Path string `json:"path"`
	// Paths overrides Path per runtime.GOOS ("darwin", "windows", "linux"),
	// with "os" as the catch-all. Claude Desktop is the reason this exists: its
	// support directory is ~/Library/Application Support on macOS,
	// ~/AppData/Roaming on Windows and ~/.config on Linux.
	Paths map[string]string `json:"paths,omitempty"`
	// Kind selects the safe editor the engine uses:
	//   json-env     — merge into the "env" object of a JSON file
	//   json-plain   — merge top-level keys of a JSON file
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
	phBaseURL     = "{{baseUrl}}"
	phBaseURLV1   = "{{baseUrlV1}}"
	phAPIKey      = "{{apiKey}}"
	phMainModel   = "{{mainModel}}"
	phSonnetModel = "{{sonnetModel}}"
	phFastModel   = "{{fastModel}}"
	phCodexModel  = "{{codexModel}}"
)

func expand(v string, o Options) string {
	return strings.NewReplacer(
		phBaseURL, o.BaseURL,
		phBaseURLV1, strings.TrimRight(o.BaseURL, "/")+"/v1",
		phAPIKey, o.APIKey,
		phMainModel, o.MainModel,
		phSonnetModel, o.SonnetModel,
		phFastModel, o.FastModel,
		phCodexModel, o.CodexModel,
	).Replace(v)
}

// DefaultSpec is what the app knows without the network. It matches what the
// server currently serves, so an offline launch behaves identically.
func DefaultSpec() Spec {
	return Spec{
		Version: 1,
		Pickers: DefaultPickers(),
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
				RestartApps: []RelaunchApp{{
					// The Codex desktop app ships inside ChatGPT.app, so the
					// bundle id is the reliable way in — matching on the app's
					// name on disk would find the wrong thing or nothing.
					BundleID: "com.openai.codex",
					Name:     "Codex desktop",
					Bins:     []string{"Codex.exe", "ChatGPT.exe"},
				}},
				Detect: DetectSpec{Dirs: []string{".codex"}, Bins: []string{"codex"}},
				Enabled: EnabledSpec{
					TomlTable: "model_providers.brewkeg",
					RootKey:   "model_provider", RootValue: "brewkeg",
					// Codex otherwise keeps its own default model, which is not
					// one we serve, and every prompt 400s.
					RootKeys: []KVSpec{{Name: "model", Value: `"` + phCodexModel + `"`}},
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
				// A directory, so it has to be checked as one. Naming it under
				// FilesByOS made the tool report "not detected" on a machine
				// where the directory plainly existed.
				Detect: DetectSpec{DirsByOS: map[string][]string{
					"darwin":  {"Library/Application Support/Claude", "Library/Application Support/Claude-3p"},
					"windows": {"AppData/Roaming/Claude", "AppData/Local/Claude-3p"},
					"linux":   {".config/Claude", ".config/Claude-3p"},
				}},
				// Detected, not verifiable. We can see the saved inference
				// configuration, so we CAN write it — see the desktop-3p file
				// below. What we still cannot do is prove the app is using it
				// until it is relaunched.
				Enabled: EnabledSpec{JSONFileKeys: []string{"inferenceGatewayBaseUrl"}},
				Note:    "set in the app",
				Files: []FileSpec{{
					// The saved third-party configuration. Its path is not
					// fixed: it is the file named by configLibrary/_meta.json,
					// so the engine resolves it and edits the configuration
					// the user already has applied. Skipped entirely when
					// there is none.
					Kind: "desktop-3p",
					Entries: []KVSpec{
						{Name: "inferenceProvider", Value: `"gateway"`},
						{Name: "inferenceGatewayBaseUrl", Value: `"{{baseUrl}}"`},
						{Name: "inferenceCredentialKind", Value: `"static"`},
						{Name: "inferenceGatewayAuthScheme", Value: `"bearer"`},
						{Name: "inferenceGatewayApiKey", Value: `"{{apiKey}}"`},
						// Model discovery off: an explicit list is what makes
						// the connection test meaningful, and discovery on a
						// gateway can return ids the account cannot use.
						{Name: "modelDiscoveryEnabled", Value: "false"},
						{Name: "inferenceModels", Value: `["{{mainModel}}","{{sonnetModel}}","{{fastModel}}"]`},
					},
				}, {
					// The support directory differs per OS. Paths are all
					// relative to $HOME, so there is no leading slash and no
					// sudo anywhere in this.
					Paths: map[string]string{
						"darwin":  "Library/Application Support/Claude/developer_settings.json",
						"windows": "AppData/Roaming/Claude/developer_settings.json",
						"linux":   ".config/Claude/developer_settings.json",
					},
					Kind: "json-plain",
					Entries: []KVSpec{
						{Name: "allowDevTools", Value: "true"},
					},
				}},
				RestartApps: []RelaunchApp{{
					BundleID: "com.anthropic.claudefordesktop",
					Name:     "Claude Desktop",
					Bins:     []string{"Claude.exe", "claude.exe"},
				}},
				ManualSteps: []ManualStep{
					{
						// The profile is created for the user now, so there is no
						// manual step left. Developer Mode only governs whether the
						// menu is visible in the app — worth saying once if it is
						// off, because otherwise a picker that does not appear has
						// no explanation.
						When: "claude-desktop-developer-mode-off",
						Lines: []string{
							"Written to your Claude Desktop configuration. Developer Mode is off, so you will not see the third-party menu:",
							"  Help -> Troubleshooting -> Enable Developer Mode, then reopen the app",
						},
					},
				},
			},
			{
				// Z.ai's coding agent. The desktop app, `zcode --web` and the
				// terminal all read one provider registry, so this is a single
				// write rather than a desktop file and a CLI file.
				ID:    "zcode",
				Label: "ZCode",
				Detect: DetectSpec{
					Dirs: []string{".zcode/v2"},
				},
				Files: []FileSpec{{
					Paths: map[string]string{
						"darwin":  ".zcode/v2/provider_config.json",
						"linux":   ".zcode/v2/provider_config.json",
						"windows": ".zcode/v2/provider_config.json",
					},
					Kind: "zcode-provider",
				}},
				Enabled: EnabledSpec{ZCodeProvider: ".zcode/v2/provider_config.json"},
				RestartApps: []RelaunchApp{{
					// ZCode's CLI lives inside the app bundle, so matching on the
					// bundle id is the only reliable way in.
					BundleID: "dev.zcode.app",
					Name:     "ZCode",
					Bins:     []string{"zcode.exe", "zcode"},
				}},
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

// DefaultPickers replaces Claude Code's model picker with brewkeg's catalogue.
// Kept in step with what the server serves; the server's copy wins when it is
// reachable, so the two can never disagree about which models exist.
func DefaultPickers() map[string]ModelPickerSpec {
	return map[string]ModelPickerSpec{
		"claude-cli": {
			Path:    ".claude/settings.json",
			Replace: true,
			Options: []PickerOption{
				{Model: "claude-opus-5", Label: "Opus 5 · brewkeg", Description: "Deepest work on hard codebases · 1M context"},
				{Model: "claude-sonnet-5", Label: "Sonnet 5 · brewkeg", Description: "Everyday coding · 1M context"},
				{Model: "claude-haiku-4-5", Label: "Haiku 4.5 · brewkeg", Description: "Quick checks and edits · 200K context"},
			},
		},
	}
}

// claudeEnvEntries are the variables that point Claude Code at brewkeg.
//
// ANTHROPIC_DEFAULT_MODEL is the one people miss. Claude Code's model picker
// keeps a "Default" row no matter what replaceBuiltInOptions says — the rows it
// drops are only those whose value is set — and that row renders "Use the
// default model (currently X)". X comes from ANTHROPIC_DEFAULT_MODEL, not from
// ANTHROPIC_MODEL, so with only the latter set the picker advertises Anthropic's
// own default (Opus 5.5) — a model brewkeg does not serve. Verified against
// 2.1.293 in a clean HOME: with neither set the request goes to
// claude-opus-5-5; setting this one in settings.json moves it to ours.
// ANTHROPIC_MODEL still wins for the actual request, so both carry the same
// value and the row and the request can never disagree.
func claudeEnvEntries() []KVSpec {
	return []KVSpec{
		{Name: "ANTHROPIC_BASE_URL", Value: `"{{baseUrl}}"`},
		{Name: "ANTHROPIC_AUTH_TOKEN", Value: `"{{apiKey}}"`},
		{Name: "ANTHROPIC_DEFAULT_MODEL", Value: `"{{mainModel}}"`},
		{Name: "ANTHROPIC_MODEL", Value: `"{{mainModel}}"`},
		{Name: "ANTHROPIC_SMALL_FAST_MODEL", Value: `"{{fastModel}}"`},
	}
}

// resolvePath expands a spec path against $HOME.
// PathForOS picks this file's path for the OS we are running on.
func (f FileSpec) PathForOS() string { return pickByOS(f.Paths, f.Path) }

// DetectPaths returns every path worth probing to decide whether the tool is
// installed here: the per-OS overrides, plus Path when it is not shadowed.
// DirPaths is DetectPaths for directories, with the same "os" fallback and the
// same rule that a key naming a non-default path shadows the untargeted list.
func (d DetectSpec) DirPaths() []string {
	if len(d.DirsByOS) == 0 {
		return nil
	}
	shadowed := map[string]bool{}
	for _, list := range d.DirsByOS {
		for _, v := range list {
			if v != "" {
				shadowed[v] = true
			}
		}
	}
	out := []string{}
	for _, list := range d.DirsByOS {
		for _, v := range list {
			if v != "" {
				out = append(out, v)
			}
		}
	}
	for _, v := range d.Dirs {
		if v != "" && !shadowed[v] {
			out = append(out, v)
		}
	}
	return out
}

func (d DetectSpec) DetectPaths() []string {
	out := make([]string, 0, len(d.Files)+len(d.FilesByOS))
	shadowed := map[string]bool{}
	for _, v := range d.FilesByOS {
		if v != "" {
			shadowed[v] = true
		}
	}
	for _, v := range d.FilesByOS {
		if v != "" {
			out = append(out, v)
		}
	}
	for _, f := range d.Files {
		if !shadowed[f] {
			out = append(out, f)
		}
	}
	return out
}

// pickByOS resolves a per-OS map, falling back to the OS-agnostic default.
func pickByOS(m map[string]string, fallback string) string {
	if v, ok := m[runtime.GOOS]; ok && v != "" {
		return v
	}
	if v, ok := m["os"]; ok && v != "" {
		return v
	}
	return fallback
}

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
