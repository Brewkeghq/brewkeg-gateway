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
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"-"`
	// MainModel is the default, SonnetModel the middle of the lineup, and
	// FastModel the cheap one. Claude Desktop needs all three: its picker has
	// fixed per-family slots and shows a slot only when a model resolves to
	// it, so a two-model lineup silently removes Sonnet from the switcher.
	MainModel   string `json:"mainModel"`
	SonnetModel string `json:"sonnetModel"`
	FastModel   string `json:"fastModel"`
}

func (o Options) WithDefaults() Options {
	if o.BaseURL == "" {
		o.BaseURL = BaseURL()
	}
	if o.MainModel == "" {
		o.MainModel = "claude-opus-5"
	}
	if o.SonnetModel == "" {
		o.SonnetModel = "claude-sonnet-5"
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
	for _, d := range t.spec.Detect.DirPaths() {
		if DirExists(resolvePath(d)) {
			return true
		}
	}
	for _, f := range t.spec.Detect.DetectPaths() {
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
		if f.PathForOS() == "" {
			continue
		}
		return resolvePath(f.PathForOS())
	}
	return ""
}

func (t specTarget) ManualInstructions(o Options) string {
	if len(t.spec.ManualSteps) > 0 {
		var b strings.Builder
		for _, s := range t.spec.ManualSteps {
			if !manualConditionHolds(s.When) {
				continue
			}
			for _, l := range s.Lines {
				b.WriteString(expand(l, o))
				b.WriteString("\n")
			}
		}
		if s := strings.TrimRight(b.String(), "\n"); s != "" {
			return s
		}
	}
	if t.spec.Manual == "" {
		return ""
	}
	return expand(t.spec.Manual, o)
}

// manualConditionHolds answers the state questions the server spec can ask
// about. Each id is a fact we can read from the machine right now; the spec
// decides what to say about it.
//
// Empty means always, so a spec block with no condition cannot be silently
// dropped by a caller that forgets to special-case it.
//
// An id this build does not know is false on purpose. A newer server sending a
// condition an older engine cannot evaluate must skip that block, not crash and
// not print a step whose precondition was never checked.
func manualConditionHolds(when string) bool {
	switch when {
	case "":
		return true
	case "claude-desktop-developer-mode-off":
		return !ClaudeDesktopDeveloperModeOn()
	case "claude-desktop-profile-missing":
		return ClaudeDesktopAppliedConfig() == ""
	default:
		return false
	}
}

// Apply writes every file the spec claims for this target, using the safe
// editor each kind maps to. The target's own config is never replaced wholesale.
func (t specTarget) Apply(b *Backup, o Options) (string, error) {
	var written []string
	var err error

	for _, f := range t.spec.Files {
		path := ""
		if f.PathForOS() != "" {
			path = resolvePath(f.PathForOS())
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

		// A path the engine has to discover rather than be told.
		if f.Kind == "desktop-3p" {
			if path, err = ClaudeDesktopEnsureProfile(); err != nil {
				return "", err
			}
		}

		switch f.Kind {
		case "json-env":
			err = writeJSONEnv(path, f.Entries, o)
		case "json-plain":
			err = writeJSONPlain(path, f.Entries, o)
		case "desktop-3p":
			err = writeJSONPlain(path, f.Entries, o)
		case "shell-block":
			err = writeShellBlock(path, f.Entries, o)
		case "toml-provider":
			err = writeTOMLProvider(path, f.Entries, o, t.spec.Enabled.RootKey, t.spec.Enabled.RootValue)
		case "zcode-provider":
			err = WriteZCodeProvider(path, o)
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
		if len(e.JSONFileKeys) > 0 && JSONHasAnyKey(ClaudeDesktopAppliedConfig(), e.JSONFileKeys) {
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
		if e.ZCodeProvider != "" && ZCodePointsAtBrewkeg(resolvePath(e.ZCodeProvider)) {
			return true
		}
		// The picker is a separate write to the same file, so a tool can be
		// configured through it alone — which is exactly the state a restore
		// that missed the picker leaves behind. Without this the switch reads
		// off, eviction skips it, and a reset leaves the rows in place.
		if p, ok := s.Pickers[ts.ID]; ok && PickerInstalled(resolvePath(p.Path)) {
			return true
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
	// Removed means this run took brewkeg *out* of the tool rather than putting
	// it in. The UI has to say which happened: a user who untoggled a service
	// and then reads "Configured Claude Code" has been told the opposite of the
	// truth.
	Removed bool `json:"removed,omitempty"`
	// Changed means this run's write actually altered a file. A target that was
	// already configured to exactly these values rewrites identical bytes and is
	// reported unchanged, so toggling one service cannot restart the fleet:
	// the desktop app restarts only the Changed ones.
	Changed bool `json:"changed,omitempty"`
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

	// Anything currently configured that the user just switched *off* comes
	// back out now, inside the same backup, so one Undo reverts the whole run
	// rather than half of it.
	for _, r := range evictTargets(s, wanted(ids), b) {
		if r.OK {
			b.Targets = append(b.Targets, r.ID)
		}
		results = append(results, r)
	}

	if err := b.Save(); err != nil {
		return b, results, err
	}

	// Only now, with every write landed, can we tell which targets really moved.
	// Asking earlier would call a rewrite of identical bytes a change and put
	// every configured app back in the restart list on every run.
	changed := b.ChangedTargets()
	for i := range results {
		if results[i].OK {
			results[i].Changed = changed[results[i].ID]
		}
	}

	// Remember the key so the next launch opens with it filled in. Only once a
	// write actually landed — a run that failed everywhere left the machine
	// untouched, so claiming to know its key would be a lie. A run that only
	// *removed* brewkeg did not put a key anywhere either, so it does not count.
	if anyWritten(results) {
		_ = StoreKey(opts.APIKey)
	}
	return b, results, nil
}

// wanted is the set of ids this run should leave connected. Anything absent is
// evicted, so "off" has to be expressible — an empty list is a valid request
// meaning "disconnect everything", not "do nothing".
func wanted(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

func anyOK(rs []ApplyResult) bool {
	for _, r := range rs {
		if r.OK {
			return true
		}
	}
	return false
}

// anyWritten is anyOK minus the removals: "did we put a key into a file".
func anyWritten(rs []ApplyResult) bool {
	for _, r := range rs {
		if r.OK && !r.Removed {
			return true
		}
	}
	return false
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

// StoredKey lives in keystore.go; it reads the key store first and falls back
// to the Claude Code settings.

func runtimeIsWindows() bool { return isWindows() }

var tomlKeyRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

var _ = tomlKeyRe

/* ------------------------------------------------------------------ restart */

// RestartHint is one "you need to do this for the change to take" line.
type RestartHint struct {
	// What is the thing that has to change, e.g. "Claude Code".
	What string `json:"what"`
	// Action is the shortest true instruction, e.g. "quit and reopen".
	Action string `json:"action"`
}

// RestartHintsFor lists what has to be restarted after configuring these
// targets. Nothing here is decoration: a config file is only read at process
// start, so a user who skips this sees no change and assumes we broke it.
//
// The shell is the easy one to miss — we wrote exports into their rc file, and
// an already-open terminal still holds the old values.
func RestartHintsFor(ids []string, changedPaths []string) []RestartHint {
	have := func(id string) bool {
		for _, x := range ids {
			if x == id {
				return true
			}
		}
		return false
	}
	hasPath := func(sub string) bool {
		for _, p := range changedPaths {
			if strings.Contains(p, sub) {
				return true
			}
		}
		return false
	}

	var hints []RestartHint
	if have("claude-cli") {
		hints = append(hints, RestartHint{"Claude Code", "quit and reopen"})
	}
	if have("codex") {
		hints = append(hints, RestartHint{"Codex CLI", "quit and reopen"})
	}
	if have("desktop") {
		hints = append(hints, RestartHint{"Claude Desktop", "quit and reopen"})
	}
	if hasPath(".zshrc") || hasPath(".bashrc") || hasPath(".bash_profile") ||
		hasPath("config.fish") || hasPath("config.nu") || hasPath(".ps1") {
		hints = append(hints, RestartHint{"Your terminal", "open a new window"})
	}
	return hints
}

// writeJSONPlain merges brewkeg's keys into the top level of a JSON file,
// preserving every other key. This is for config that is not a bag of
// environment variables — Claude Desktop's developer_settings.json is a flat
// object of switches.
func writeJSONPlain(path string, entries []KVSpec, o Options) error {
	doc := map[string]any{}
	if raw := ReadFile(path); strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			return fmt.Errorf("%s is not valid JSON — fix or move it, then re-run", path)
		}
	}
	for _, e := range entries {
		doc[e.Name] = typedValue(expand(e.Value, o))
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(path, string(out)+"\n")
}

// typedValue keeps a spec value's JSON type instead of stringifying it.
//
// This matters more than it looks: "allowDevTools": "true" is a string to
// Electron, which is a different thing from a boolean, and the app would keep
// ignoring the setting with no error anywhere. A bare true/false/null/number
// stays literal; anything unparseable is written as the string it is.
func typedValue(v string) any {
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) && len(v) >= 2 {
		return unquote(v)
	}
	var parsed any
	if err := json.Unmarshal([]byte(v), &parsed); err == nil {
		return parsed
	}
	return v
}

// JSONHasAnyKey reports whether a JSON file has any of these top-level keys set
// to a non-empty value. An empty file path means "no config to read", which is
// false, not an error.
func JSONHasAnyKey(path string, keys []string) bool {
	raw := ReadFile(path)
	if strings.TrimSpace(raw) == "" {
		return false
	}
	var doc map[string]any
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return false
	}
	for _, k := range keys {
		if v, ok := doc[k]; ok && v != nil && v != "" {
			return true
		}
	}
	return false
}
