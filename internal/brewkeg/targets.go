package brewkeg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Options are the knobs every target shares. Defaults come from the same facts
// the docs publish: base URL https://brewkeg.dev, Responses API for codex.
type Options struct {
	BaseURL   string `json:"baseUrl"`
	APIKey    string `json:"-"`
	MainModel string `json:"mainModel"`
	FastModel string `json:"fastModel"`
}

func (o Options) WithDefaults() Options {
	if o.BaseURL == "" {
		o.BaseURL = BaseURL()
	}
	if o.MainModel == "" {
		o.MainModel = "claude-opus-5"
	}
	if o.FastModel == "" {
		o.FastModel = "claude-haiku-4-5"
	}
	return o
}

type Target interface {
	ID() string
	Label() string
	// DetectPath returns the file this target owns, "" when it is not installed.
	DetectPath() string
	// ManualInstructions is shown for targets we must not edit by hand (GUI-only).
	ManualInstructions(opts Options) string
	Apply(b *Backup, opts Options) (string, error)
}

var targets = []Target{claudeCLITarget{}, codexTarget{}, desktopTarget{}}

func Targets() []Target { return targets }

func TargetByID(id string) (Target, bool) {
	for _, t := range targets {
		if t.ID() == id {
			return t, true
		}
	}
	return nil, false
}

func TargetIDs() []string {
	var ids []string
	for _, t := range targets {
		ids = append(ids, t.ID())
	}
	return ids
}

// TargetStatus is everything a UI needs to draw one row: is the app installed,
// is brewkeg already in its config, which files we would touch.
type TargetStatus struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Path      string `json:"path"`
	Note      string `json:"note,omitempty"`
}

func StatusAll() []TargetStatus {
	out := make([]TargetStatus, 0, len(targets))
	for _, t := range targets {
		p := t.DetectPath()
		s := TargetStatus{ID: t.ID(), Label: t.Label(), Installed: p != "", Path: p}
		switch t.ID() {
		case "claude-cli":
			s.Enabled = len(SettingsEnvKeys(p)) > 0 || (ShellRC() != "" && HasBlock(ReadFile(ShellRC())))
		case "codex":
			s.Enabled = HasBlock(ReadFile(p)) && strings.Contains(ReadFile(p), "model_providers.brewkeg")
		default:
			// Desktop is configured inside the app, so we cannot observe it.
			s.Note = "configured in the app"
		}
		out = append(out, s)
	}
	return out
}

// ApplyResult reports one target's outcome so the UI can show the truth,
// including which files actually changed.
type ApplyResult struct {
	ID     string   `json:"id"`
	Label  string   `json:"label"`
	OK     bool     `json:"ok"`
	Error  string   `json:"error,omitempty"`
	Paths  []string `json:"paths,omitempty"`
	Manual string   `json:"manual,omitempty"`
}

// Apply configures the given target ids inside one backup, and saves it. Either
// every requested target is attempted or the error is returned — the backup is
// always saved so a partial run is still restorable.
func Apply(ids []string, opts Options) (*Backup, []ApplyResult, error) {
	opts = opts.WithDefaults()
	b := NewBackup(opts.BaseURL)

	var results []ApplyResult
	for _, id := range ids {
		t, ok := TargetByID(id)
		if !ok {
			results = append(results, ApplyResult{ID: id, Error: "unknown service"})
			continue
		}
		r := ApplyResult{ID: t.ID(), Label: t.Label()}
		if p := t.DetectPath(); p != "" {
			if err := b.Capture(t.ID(), p); err != nil {
				r.Error = err.Error()
				results = append(results, r)
				continue
			}
		}
		path, err := t.Apply(b, opts)
		if err != nil {
			r.Error = err.Error()
			results = append(results, r)
			continue
		}
		r.OK = true
		r.Manual = t.ManualInstructions(opts)
		if path != "" {
			r.Paths = strings.Split(path, " + ")
		}
		b.Targets = append(b.Targets, t.ID())
		results = append(results, r)
	}

	if err := b.Save(); err != nil {
		return b, results, err
	}
	return b, results, nil
}

/* ------------------------------------------------------------------ Claude Code CLI */

type claudeCLITarget struct{}

func (claudeCLITarget) ID() string    { return "claude-cli" }
func (claudeCLITarget) Label() string { return "Claude Code CLI" }

func (claudeCLITarget) DetectPath() string {
	if FileExists(HomeJoin(".claude")) || LookPath("claude") {
		return HomeJoin(".claude", "settings.json")
	}
	return ""
}

func (claudeCLITarget) ManualInstructions(Options) string { return "" }

func (t claudeCLITarget) Apply(b *Backup, o Options) (string, error) {
	settings := HomeJoin(".claude", "settings.json")
	if err := b.Capture(t.ID(), settings); err != nil {
		return "", err
	}
	if err := writeSettingsEnv(settings, o); err != nil {
		return "", err
	}

	rc := ShellRC()
	if runtime.GOOS == "windows" && rc == "" {
		rc = WindowsProfile()
	}
	if rc == "" {
		return settings + " (skipped shell env: unknown shell, set the vars yourself)", nil
	}
	if err := b.Capture(t.ID(), rc); err != nil {
		return "", err
	}
	body := EnvExports(o, runtime.GOOS == "windows" || strings.HasSuffix(rc, ".ps1"))
	if err := WriteFile(rc, ApplyBlock(ReadFile(rc), BlockBegin, BlockEnd, body)); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s + %s", settings, rc), nil
}

func EnvExports(o Options, windows bool) string {
	if windows {
		return strings.Join([]string{
			`$env:ANTHROPIC_BASE_URL = "` + o.BaseURL + `"`,
			`$env:ANTHROPIC_AUTH_TOKEN = "` + o.APIKey + `"`,
			`$env:ANTHROPIC_MODEL = "` + o.MainModel + `"`,
			`$env:ANTHROPIC_SMALL_FAST_MODEL = "` + o.FastModel + `"`,
		}, "\n")
	}
	return strings.Join([]string{
		`export ANTHROPIC_BASE_URL="` + o.BaseURL + `"`,
		`export ANTHROPIC_AUTH_TOKEN="` + o.APIKey + `"`,
		`export ANTHROPIC_MODEL="` + o.MainModel + `"`,
		`export ANTHROPIC_SMALL_FAST_MODEL="` + o.FastModel + `"`,
	}, "\n")
}

var brewkegEnvKeys = []string{
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_MODEL",
	"ANTHROPIC_SMALL_FAST_MODEL",
}

func BrewkegEnvKeys() []string { return brewkegEnvKeys }

// writeSettingsEnv merges our vars into the "env" object of Claude Code's
// settings.json, preserving every other key and every unrelated env var.
func writeSettingsEnv(path string, o Options) error {
	doc := map[string]any{}
	if raw := ReadFile(path); strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			return fmt.Errorf("%s is not valid JSON — fix or move it, then re-run", path)
		}
	}
	env, _ := doc["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	env["ANTHROPIC_BASE_URL"] = o.BaseURL
	env["ANTHROPIC_AUTH_TOKEN"] = o.APIKey
	env["ANTHROPIC_MODEL"] = o.MainModel
	env["ANTHROPIC_SMALL_FAST_MODEL"] = o.FastModel
	doc["env"] = env

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(path, string(out)+"\n")
}

// SettingsEnvKeys lists which brewkeg vars are present in settings.json.
func SettingsEnvKeys(path string) []string {
	raw := ReadFile(path)
	if raw == "" {
		return nil
	}
	var doc struct {
		Env map[string]any `json:"env"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil
	}
	var hit []string
	for _, k := range brewkegEnvKeys {
		if _, ok := doc.Env[k]; ok {
			hit = append(hit, k)
		}
	}
	return hit
}

// StoredKey returns the brewkeg key already configured on this machine, if any.
// The UI uses it to pre-fill rather than asking for something it already has.
func StoredKey() string {
	raw := ReadFile(HomeJoin(".claude", "settings.json"))
	var doc struct {
		Env map[string]string `json:"env"`
	}
	if json.Unmarshal([]byte(raw), &doc) == nil {
		return doc.Env["ANTHROPIC_AUTH_TOKEN"]
	}
	return ""
}

/* ------------------------------------------------------------------ Codex CLI */

type codexTarget struct{}

func (codexTarget) ID() string    { return "codex" }
func (codexTarget) Label() string { return "Codex CLI" }

func (codexTarget) DetectPath() string {
	if FileExists(HomeJoin(".codex")) || LookPath("codex") {
		return HomeJoin(".codex", "config.toml")
	}
	return ""
}

func (codexTarget) ManualInstructions(Options) string { return "" }

func (t codexTarget) Apply(b *Backup, o Options) (string, error) {
	p := HomeJoin(".codex", "config.toml")
	if err := b.Capture(t.ID(), p); err != nil {
		return "", err
	}

	body := strings.Join([]string{
		"[model_providers.brewkeg]",
		`name = "brewkeg"`,
		`base_url = "` + o.BaseURL + `/v1"`,
		`wire_api = "responses"`,
		`experimental_bearer_token = "` + o.APIKey + `"`,
	}, "\n")

	next := ApplyBlock(ReadFile(p), BlockBegin, BlockEnd, body)
	next = SetRootKey(next, "model_provider", `"brewkeg"`)
	if err := WriteFile(p, next); err != nil {
		return "", err
	}
	return p, nil
}

/* ------------------------------------------------------------------ Claude Desktop */

type desktopTarget struct{}

func (desktopTarget) ID() string    { return "desktop" }
func (desktopTarget) Label() string { return "Claude Desktop" }

func (desktopTarget) DetectPath() string {
	if runtime.GOOS == "darwin" {
		p := HomeJoin("Library", "Application Support", "Claude", "config.json")
		if FileExists(p) {
			return p
		}
	}
	if runtime.GOOS == "windows" {
		p := filepath.Join(os.Getenv("APPDATA"), "Claude", "config.json")
		if FileExists(p) {
			return p
		}
	}
	return ""
}

// Claude Desktop configures its gateway through a Developer menu, not a file
// we can safely rewrite — its config.json holds MCP servers, not inference
// settings. So we print exact steps instead of guessing.
func (desktopTarget) ManualInstructions(o Options) string {
	return strings.Join([]string{
		"1. Open Claude Desktop",
		"2. Developer menu -> Configure Third-Party Inference...",
		"3. Base URL: " + o.BaseURL,
		"4. Gateway API key: " + MaskKey(o.APIKey),
		"5. Apply Changes, then fully quit and reopen the app",
	}, "\n")
}

func (t desktopTarget) Apply(b *Backup, o Options) (string, error) {
	return t.DetectPath(), nil
}
