package brewkeg

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Options are the knobs every target shares. Defaults match what the docs
// publish: base URL https://brewkeg.dev, Responses API for codex.
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

// Target is one tool brewkeg can point at itself. Implementations are built
// from a Spec, so the app can learn about a new tool — or a changed config
// shape — without a new release.
type Target interface {
	ID() string
	Label() string
	Icon() string
	// DetectPath returns the primary config file this target owns, "" when the
	// tool is not installed or owns no file.
	DetectPath() string
	// Installed is whether the tool is present on this machine at all.
	Installed() bool
	ManualInstructions(opts Options) string
	Apply(b *Backup, opts Options) (string, error)
}

type specTarget struct{ spec TargetSpec }

func (t specTarget) ID() string    { return t.spec.ID }
func (t specTarget) Label() string { return t.spec.Label }
func (t specTarget) Icon() string  { return t.spec.Icon }

func (t specTarget) Installed() bool {
	for _, d := range t.spec.Detect.Dirs {
		if DirExists(resolvePath(d)) {
			return true
		}
	}
	for _, f := range t.spec.Detect.Files {
		if FileExists(resolvePath(f)) {
			return true
		}
	}
	for _, b := range t.spec.Detect.Bins {
		if LookPath(b) {
			return true
		}
	}
	return false
}

func (t specTarget) DetectPath() string {
	if !t.Installed() {
		return ""
	}
	for _, f := range t.spec.Files {
		if f.Path == "" {
			continue
		}
		return resolvePath(f.Path)
	}
	return ""
}

func (t specTarget) ManualInstructions(o Options) string {
	if t.spec.Manual == "" {
		return ""
	}
	return expand(t.spec.Manual, o)
}

// Apply writes every file the spec claims for this target, using the safe
// editor each kind maps to. The target's own config is never replaced wholesale.
func (t specTarget) Apply(b *Backup, o Options) (string, error) {
	var written []string

	for _, f := range t.spec.Files {
		path := ""
		if f.Path != "" {
			path = resolvePath(f.Path)
		} else if f.Kind == "shell-block" {
			path = ShellRC()
			if path == "" {
				// Unknown shell: never guess and corrupt a file.
				continue
			}
		}

		if err := b.Capture(t.ID(), path); err != nil {
			return "", err
		}

		var err error
		switch f.Kind {
		case "json-env":
			err = writeJSONEnv(path, f.Entries, o)
		case "shell-block":
			err = writeShellBlock(path, f.Entries, o)
		case "toml-provider":
			err = writeTOMLProvider(path, f.Entries, o, t.spec.Enabled.RootKey, t.spec.Enabled.RootValue)
		default:
			err = fmt.Errorf("unknown file kind %q in spec", f.Kind)
		}
		if err != nil {
			return "", err
		}
		written = append(written, path)
	}

	if t.spec.Enabled.ShellBlock {
		if cleared, err := ClearStaleGatewayCaches(b); err == nil {
			for _, c := range cleared {
				written = append(written, "cleared "+c)
			}
		}
	}

	if len(written) == 0 {
		return "", nil
	}
	return strings.Join(written, " + "), nil
}

// applyTarget writes a target's config, then its model picker. The picker is a
// separate write because it targets a tool's model catalogue rather than its
// connection settings, and it must survive a config file that the tool rewrites.
func applyTarget(t Target, s Spec, b *Backup, opts Options) (string, error) {
	path, err := t.Apply(b, opts)
	if err != nil {
		return "", err
	}
	if p, ok := s.Pickers[t.ID()]; ok && len(p.Options) > 0 {
		pp := resolvePath(p.Path)
		if err := b.Capture(t.ID(), pp); err != nil {
			return "", err
		}
		if err := applyPicker(pp, p); err != nil {
			return "", err
		}
		if path != "" && !strings.Contains(path, pp) {
			path = path + " + " + pp
		} else if path == "" {
			path = pp
		}
	}
	return path, nil
}

/* ------------------------------------------------------------------ editors */

// writeJSONEnv merges brewkeg's variables into the "env" object of a JSON
// settings file, preserving every other key and every unrelated env var.
func writeJSONEnv(path string, entries []KVSpec, o Options) error {
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
	for _, e := range entries {
		env[e.Name] = unquote(expand(e.Value, o))
	}
	doc["env"] = env

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(path, string(out)+"\n")
}

// writeShellBlock keeps brewkeg's exports inside one marked block, appended so
// it wins over any earlier assignment of the same variable.
func writeShellBlock(path string, entries []KVSpec, o Options) error {
	windows := strings.HasSuffix(path, ".ps1") || runtimeIsWindows()
	var lines []string
	for _, e := range entries {
		val := unquote(expand(e.Value, o))
		if windows {
			lines = append(lines, fmt.Sprintf("$env:%s = %q", e.Name, val))
		} else {
			lines = append(lines, fmt.Sprintf("export %s=%q", e.Name, val))
		}
	}
	return WriteFile(path, ApplyBlock(ReadFile(path), BlockBegin, BlockEnd, strings.Join(lines, "\n")))
}

// writeTOMLProvider edits [model_providers.<name>] in place when it already
// exists, and otherwise drops a marked block at the end. Appending a second
// table of the same name would be a parse error, so that distinction matters.
func writeTOMLProvider(path string, entries []KVSpec, o Options, rootKey, rootValue string) error {
	table := "model_providers.brewkeg"
	kv := make([]string, 0, len(entries))
	for _, e := range entries {
		kv = append(kv, e.Name+" = "+expand(e.Value, o))
	}

	current := ReadFile(path)
	next, edited := SetTableKeys(current, table, kv)
	if !edited {
		next = ApplyBlock(current, BlockBegin, BlockEnd, "["+table+"]\n"+strings.Join(kv, "\n"))
	}
	if rootKey != "" && rootValue != "" {
		next = SetRootKey(next, rootKey, `"`+rootValue+`"`)
	}
	return WriteFile(path, next)
}

// unquote strips the JSON string quoting a spec value carries, so callers can
// treat values as plain strings and never hand-format them into a file.
func unquote(v string) string {
	if len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
		var out string
		if err := json.Unmarshal([]byte(v), &out); err == nil {
			return out
		}
	}
	return v
}

/* ------------------------------------------------------------------ status */

// TargetStatus is everything a UI needs to draw one row: is the tool installed,
// is brewkeg already in its config, which files we would touch.
type TargetStatus struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Icon      string `json:"icon,omitempty"`
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Path      string `json:"path"`
	Note      string `json:"note,omitempty"`
}

func StatusAll() []TargetStatus { return StatusAllFrom(DefaultSpec()) }

func StatusAllFrom(s Spec) []TargetStatus {
	out := make([]TargetStatus, 0, len(s.Targets))
	for _, t := range TargetsFrom(s) {
		st := TargetStatus{
			ID: t.ID(), Label: t.Label(), Icon: t.Icon(),
			Installed: t.Installed(), Path: t.DetectPath(),
			Enabled: enabledFor(t, s), Note: noteFor(t),
		}
		if st.Enabled {
			if hits := ShellRCsWithBrewkegBlock(); len(hits) > 0 && st.Path != "" {
				st.Path = st.Path + " + " + strings.Join(hits, " + ")
			}
		}
		out = append(out, st)
	}
	return out
}

func noteFor(t Target) string {
	if st, ok := t.(specTarget); ok {
		return st.spec.Note
	}
	return ""
}

// enabledFor reads the tool's own config to decide the switch state. It never
// looks for our marker comments: someone who wired brewkeg in by hand must see
// the same switch state as someone who used the app.
func enabledFor(t Target, s Spec) bool {
	for _, ts := range s.Targets {
		if ts.ID != t.ID() {
			continue
		}
		e := ts.Enabled
		if e.NeverDetectable {
			return false
		}
		if len(e.JSONEnvKeys) > 0 && len(SettingsEnvKeysAny(t.DetectPath(), e.JSONEnvKeys)) > 0 {
			return true
		}
		if e.ShellBlock && len(ShellRCsWithBrewkegBlock()) > 0 {
			return true
		}
		if e.TomlTable != "" {
			content := ReadFile(t.DetectPath())
			if TableHas(content, e.TomlTable) {
				if e.RootKey == "" || RootKeyIs(content, e.RootKey, e.RootValue) {
					return true
				}
			}
		}
		return false
	}
	return false
}

func Targets() []Target { return TargetsFrom(DefaultSpec()) }

func TargetsFrom(s Spec) []Target {
	out := make([]Target, 0, len(s.Targets))
	for _, ts := range s.Targets {
		out = append(out, specTarget{spec: ts})
	}
	return out
}

func TargetByID(id string) (Target, bool) {
	return TargetByIDFrom(DefaultSpec(), id)
}

func TargetByIDFrom(s Spec, id string) (Target, bool) {
	for _, t := range TargetsFrom(s) {
		if t.ID() == id {
			return t, true
		}
	}
	return nil, false
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
// always saved, so a partial run is still restorable.
func Apply(ids []string, opts Options) (*Backup, []ApplyResult, error) {
	return ApplyWithSpec(DefaultSpec(), ids, opts)
}

func ApplyWithSpec(s Spec, ids []string, opts Options) (*Backup, []ApplyResult, error) {
	opts = opts.WithDefaults()
	b := NewBackup(opts.BaseURL)

	var results []ApplyResult
	for _, id := range ids {
		t, ok := TargetByIDFrom(s, id)
		if !ok {
			results = append(results, ApplyResult{ID: id, Error: "unknown service"})
			continue
		}
		r := ApplyResult{ID: t.ID(), Label: t.Label()}
		path, err := applyTarget(t, s, b, opts)
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

/* ------------------------------------------------------------------ caches */

// StaleGatewayCaches are caches keyed to a gateway URL. Claude Code fetches the
// model list once and caches it under the baseUrl it came from; switch the base
// URL and the old list keeps being served, so the picker still advertises models
// the new gateway does not have.
func StaleGatewayCaches() []string {
	return []string{
		HomeJoin(".claude", "cache", "gateway-models.json"),
	}
}

// ClearStaleGatewayCaches removes those caches. The copies stay in the backup,
// so a restore clears them rather than resurrecting a list for a gateway the
// user just left.
func ClearStaleGatewayCaches(b *Backup) ([]string, error) {
	var cleared []string
	for _, p := range StaleGatewayCaches() {
		if !FileExists(p) {
			continue
		}
		if err := b.Capture("cache", p); err != nil {
			return cleared, err
		}
		for i := range b.Entries {
			if b.Entries[i].Path == p {
				b.Entries[i].Transient = true
			}
		}
		if err := os.Remove(p); err != nil {
			return cleared, fmt.Errorf("clearing %s: %w", p, err)
		}
		cleared = append(cleared, p)
	}
	return cleared, nil
}

// StaleGatewayCachesPending lists caches that exist right now.
func StaleGatewayCachesPending() []string {
	var found []string
	for _, p := range StaleGatewayCaches() {
		if FileExists(p) {
			found = append(found, p)
		}
	}
	return found
}

func SettingsEnvKeys(path string) []string {
	return SettingsEnvKeysAny(path, brewkegEnvKeys)
}

// SettingsEnvKeysAny lists which of the named keys are in a settings.json env.
func SettingsEnvKeysAny(path string, keys []string) []string {
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
	for _, k := range keys {
		if _, ok := doc.Env[k]; ok {
			hit = append(hit, k)
		}
	}
	return hit
}

// BrewkegEnvKeys are the Claude Code variables brewkeg owns today. Detection
// uses the spec's list; this stays for callers that ask directly.
func BrewkegEnvKeys() []string { return brewkegEnvKeys }

var brewkegEnvKeys = []string{
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_MODEL",
	"ANTHROPIC_SMALL_FAST_MODEL",
}

// StoredKey returns the brewkeg key already configured on this machine, if any.
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

func runtimeIsWindows() bool { return isWindows() }

var tomlKeyRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

var _ = tomlKeyRe
