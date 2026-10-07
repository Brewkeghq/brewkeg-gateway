package brewkeg

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchSpecReadsServerConfig(t *testing.T) {
	want := Spec{Version: 99, Targets: []TargetSpec{{ID: "codex", Label: "Codex CLI"}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/client-config" {
			t.Errorf("spec should be fetched from /api/client-config, got %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	got, err := FetchSpec(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 99 || got.Targets[0].ID != "codex" {
		t.Fatalf("server spec not used: %+v", got)
	}
}

// A dead or slow gateway must never stop someone configuring their tools.
func TestFetchSpecFallsBackOffline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	got, err := FetchSpec(context.Background(), srv.URL)
	if err == nil {
		t.Error("a 500 should be reported as an error")
	}
	if len(got.Targets) == 0 {
		t.Fatal("must fall back to the built-in spec")
	}
	if got.Version != DefaultSpec().Version {
		t.Errorf("fallback version = %d", got.Version)
	}
}

// The whole point of the spec: a server change must reach an installed app
// without a new release, and the change must be applied safely.
func TestServerSpecChangesCodexWithoutNewRelease(t *testing.T) {
	h := home(t)
	toml := "model = \"gpt-5\"\n\n[tui]\ntheme = \"dark\"\n"
	p := filepath.Join(h, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	// The server decides codex now needs a different wire_api and an extra key.
	server := DefaultSpec()
	for i := range server.Targets {
		if server.Targets[i].ID != "codex" {
			continue
		}
		server.Targets[i].Files[0].Entries = append(server.Targets[i].Files[0].Entries,
			KVSpec{Name: "requires_openai_auth", Value: "false"})
		for j, e := range server.Targets[i].Files[0].Entries {
			if e.Name == "wire_api" {
				server.Targets[i].Files[0].Entries[j] = KVSpec{Name: "wire_api", Value: `"chat"`}
			}
		}
	}
	server.Version = DefaultSpec().Version + 1

	if _, _, err := ApplyWithSpec(server, []string{"codex"}, Options{APIKey: "bk_live_K"}); err != nil {
		t.Fatal(err)
	}

	got := ReadFile(p)
	if !strings.Contains(got, `wire_api = "chat"`) {
		t.Errorf("server's new wire_api did not reach the file:\n%s", got)
	}
	if !strings.Contains(got, "requires_openai_auth = false") {
		t.Errorf("server's new key did not reach the file:\n%s", got)
	}
	if !strings.Contains(got, "[tui]") || !strings.Contains(got, `model = "gpt-5"`) {
		t.Errorf("the user's own config was not preserved:\n%s", got)
	}
}

func TestFetchSpecRefusesOlderThanBuiltIn(t *testing.T) {
	old := Spec{Version: 0, Targets: DefaultSpec().Targets}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(old)
	}))
	defer srv.Close()

	got, err := FetchSpec(context.Background(), srv.URL)
	if err == nil {
		t.Error("an older server spec must be refused, not silently downgrade the app")
	}
	if got.Version != DefaultSpec().Version {
		t.Errorf("should have kept the built-in spec, got v%d", got.Version)
	}
}

func TestSpecIsTheOnlyPlacePathsAndKeysLive(t *testing.T) {
	// A new tool must be addable by editing data, not Go. If this fails, the
	// spec is not actually driving the engine.
	spec := DefaultSpec()
	spec.Targets = append(spec.Targets, TargetSpec{
		ID: "opencode", Label: "OpenCode", Icon: "brewkeg",
		Detect: DetectSpec{Dirs: []string{".opencode"}},
		Enabled: EnabledSpec{
			JSONEnvKeys: []string{"OPENCODE_BASE_URL"},
		},
		Files: []FileSpec{{Path: ".opencode/config.json", Kind: "json-env",
			Entries: []KVSpec{{Name: "OPENCODE_BASE_URL", Value: `"{{baseUrlV1}}"`}}}},
	})

	tgts := TargetsFrom(spec)
	if len(tgts) != len(spec.Targets) {
		t.Fatalf("expected %d targets from spec, got %d", len(spec.Targets), len(tgts))
	}
	if _, ok := TargetByIDFrom(spec, "opencode"); !ok {
		t.Fatal("a tool that exists only in the spec must be selectable")
	}
}

// The picker block lives inside a settings file that also holds env, hooks and
// permissions. Replacing the picker must not disturb any of them.
func TestPickerReplacesOptionsAndKeepsEverythingElse(t *testing.T) {
	h := home(t)
	settings := filepath.Join(h, ".claude", "settings.json")
	before := `{
  "permissions": { "allow": ["Bash"] },
  "hooks": { "PreToolUse": [] },
  "modelPicker": {
    "replaceBuiltInOptions": false,
    "options": [{ "model": "corporate-sonnet-v1", "label": "Company Sonnet" }]
  },
  "env": { "KEEP": "me" }
}`
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := DefaultSpec()
	spec.Pickers = map[string]ModelPickerSpec{
		"claude-cli": {
			Path:    ".claude/settings.json",
			Replace: true,
			Options: []PickerOption{
				{Model: "claude-opus-5", Label: "Opus 5 · brewkeg", Description: "200K context"},
				{Model: "claude-haiku-4-5", Label: "Haiku 4.5 · brewkeg"},
			},
		},
	}

	b, _, err := ApplyWithSpec(spec, []string{"claude-cli"}, Options{APIKey: "bk_live_K"})
	if err != nil {
		t.Fatal(err)
	}

	got := ReadFile(settings)
	for _, keep := range []string{`"allow"`, `"PreToolUse"`, `"KEEP": "me"`, "ANTHROPIC_BASE_URL"} {
		if !strings.Contains(got, keep) {
			t.Errorf("picker write lost %q:\n%s", keep, got)
		}
	}
	if strings.Contains(got, "corporate-sonnet-v1") {
		t.Errorf("the old picker option should be gone when replace is true:\n%s", got)
	}
	if !strings.Contains(got, `"replaceBuiltInOptions": true`) {
		t.Errorf("replaceBuiltInOptions should be set:\n%s", got)
	}
	if n := strings.Count(got, `"model":`); n != 2 {
		t.Errorf("expected exactly the 2 brewkeg options, got %d:\n%s", n, got)
	}

	if _, err := b.Restore(false); err != nil {
		t.Fatal(err)
	}
	if after := ReadFile(settings); after != before {
		t.Errorf("restore should put the original picker back:\n got: %s\nwant: %s", after, before)
	}
}

// A picker with no options must not wipe the user's list.
func TestEmptyPickerLeavesFileAlone(t *testing.T) {
	h := home(t)
	settings := filepath.Join(h, ".claude", "settings.json")
	before := `{"modelPicker":{"options":[{"model":"mine"}]}}`
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := DefaultSpec()
	spec.Pickers = map[string]ModelPickerSpec{"claude-cli": {Path: ".claude/settings.json", Replace: true}}
	if _, _, err := ApplyWithSpec(spec, []string{"claude-cli"}, Options{APIKey: "bk_live_K"}); err != nil {
		t.Fatal(err)
	}
	if got := ReadFile(settings); !strings.Contains(got, "mine") {
		t.Errorf("an empty picker must not clear the user's options:\n%s", got)
	}
}

// The spec is the contract between web2/lib/client-config.ts and this package.
// A field renamed on one side decodes to nothing on the other, and the app
// quietly falls back to its built-in copy — which is exactly the drift this
// design is supposed to make impossible. Pin the wire names.
func TestSpecUsesTheNamesTheServerSends(t *testing.T) {
	withTempHome(t)
	raw, err := json.Marshal(DefaultSpec())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Targets []struct {
			ID     string `json:"id"`
			Detect struct {
				FilesByOS map[string]string   `json:"filesByOS"`
				DirsByOS  map[string][]string `json:"dirsByOS"`
			} `json:"detect"`
			Files []struct {
				Paths   map[string]string `json:"paths"`
				Kind    string            `json:"kind"`
				Entries []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"entries"`
			} `json:"files"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	// detect.filesByOS is deliberately absent: every built-in target that has
	// a per-OS detect path names a directory, and a directory checked with a
	// file test never matches. The field is still honoured for an older server
	// spec that still sends it, which is why it is not deleted.
	for _, want := range []string{"dirsByOS", "paths", "kind", "entries", "jsonFileKeys"} {
		if !strings.Contains(string(raw), `"`+want+`"`) {
			t.Errorf("built-in spec never emits %q — the server may have renamed it", want)
		}
	}

	var desktop bool
	for _, tg := range doc.Targets {
		if tg.ID != "desktop" {
			continue
		}
		desktop = true
		if len(tg.Detect.DirsByOS) != 3 {
			t.Errorf("desktop detect.dirsByOS has %d entries, want 3 (darwin/windows/linux)", len(tg.Detect.DirsByOS))
		}
		// A directory checked with a file test is a tool that reports itself
		// missing on a machine where it is installed.
		if len(tg.Detect.FilesByOS) != 0 {
			t.Errorf("desktop detect.names directories in filesByOS: %v", tg.Detect.FilesByOS)
		}
		if len(tg.Files) != 2 {
			t.Fatalf("desktop files = %d, want 2 (the 3p config and the dev-tools switch)", len(tg.Files))
		}
		// The inference configuration, edited in place in the saved profile.
		gw := tg.Files[0]
		if gw.Kind != "desktop-3p" {
			t.Errorf("gateway kind = %q, want desktop-3p", gw.Kind)
		}
		wantGW := map[string]string{
			"inferenceProvider":       `"gateway"`,
			"inferenceGatewayBaseUrl": `"{{baseUrl}}"`,
			"inferenceCredentialKind": `"static"`,
			"inferenceGatewayApiKey":  `"{{apiKey}}"`,
			"modelDiscoveryEnabled":   "false",
		}
		got := map[string]string{}
		for _, e := range gw.Entries {
			got[e.Name] = e.Value
		}
		for k, v := range wantGW {
			if got[k] != v {
				t.Errorf("gateway entry %s = %s, want %s", k, got[k], v)
			}
		}
		// All three families. Claude Desktop's picker has one fixed slot per
		// family and shows a slot only when a model resolves to it, so a
		// two-model lineup silently deletes the Sonnet row from the user's
		// switcher instead of failing loudly.
		wantLineup := `["{{mainModel}}","{{sonnetModel}}","{{fastModel}}"]`
		if got["inferenceModels"] != wantLineup {
			t.Errorf("inferenceModels = %s, want %s", got["inferenceModels"], wantLineup)
		}

		// The developer-mode switch.
		f := tg.Files[1]
		if f.Kind != "json-plain" {
			t.Errorf("devtools kind = %q", f.Kind)
		}
		if len(f.Paths) != 3 {
			t.Errorf("devtools paths has %d entries, want 3", len(f.Paths))
		}
		if len(f.Entries) != 1 || f.Entries[0].Name != "allowDevTools" || f.Entries[0].Value != "true" {
			t.Errorf("devtools entries = %+v, want allowDevTools=true", f.Entries)
		}
	}
	if !desktop {
		t.Fatal("no desktop target in the built-in spec")
	}
}

// allowDevTools must land as a JSON boolean. A quoted "true" is a different
// value to Electron and fails with no error message anywhere.
func TestTypedValueKeepsJSONTypes(t *testing.T) {
	cases := []struct {
		in   string
		want any
	}{
		{"true", true},
		{"false", false},
		{"null", nil},
		{"42", float64(42)},
		{`"hello"`, "hello"},
		{"not json at all", "not json at all"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := typedValue(tc.in); got != tc.want {
			t.Errorf("typedValue(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}

// The "Default" row of Claude Code's picker is not removable: with
// replaceBuiltInOptions it keeps every row whose value is null, which is exactly
// that one. It renders "currently X" from ANTHROPIC_DEFAULT_MODEL, so if we do
// not set it the picker advertises Anthropic's own default — Opus 5.5, a model
// brewkeg does not serve, verified against 2.1.293 in a clean HOME.
func TestClaudeDefaultRowPointsAtAModelWeServe(t *testing.T) {
	withTempHome(t)

	entries := claudeEnvEntries()
	byName := map[string]string{}
	for _, e := range entries {
		byName[e.Name] = e.Value
	}

	def, ok := byName["ANTHROPIC_DEFAULT_MODEL"]
	if !ok {
		t.Fatal("ANTHROPIC_DEFAULT_MODEL is not written; the Default row will advertise a model we do not serve")
	}
	main, ok := byName["ANTHROPIC_MODEL"]
	if !ok {
		t.Fatal("ANTHROPIC_MODEL is not written")
	}

	o := Options{APIKey: "k", BaseURL: "https://brewkeg.dev", MainModel: "claude-opus-5", FastModel: "claude-haiku-4-5"}
	if got := unquote(expand(def, o)); got != "claude-opus-5" {
		t.Errorf("ANTHROPIC_DEFAULT_MODEL = %q, want the main model", got)
	}
	// ANTHROPIC_MODEL wins for the actual request, so the two must agree or the
	// row advertises one model while the session silently runs another.
	if got := unquote(expand(main, o)); got != unquote(expand(def, o)) {
		t.Errorf("ANTHROPIC_MODEL = %q but ANTHROPIC_DEFAULT_MODEL = %q; the picker would disagree with the request",
			got, unquote(expand(def, o)))
	}

	// And it must be a real catalog id, not a typed-from-memory one.
	if !strings.Contains(expand(def, o), o.MainModel) {
		t.Errorf("default model %q is not the main model %q", def, o.MainModel)
	}
}

// The spec shape test pins the wire names, so a rename on either side shows up
// there. This one exists because the failure mode is silent: the app works, the
// picker just lies about which model is the default.
func TestTheUnsupportedOpus55IsNeverWritten(t *testing.T) {
	withTempHome(t)
	o := Options{APIKey: "k", BaseURL: "https://brewkeg.dev", MainModel: "claude-opus-5", FastModel: "claude-haiku-4-5"}
	for _, e := range claudeEnvEntries() {
		got := expand(e.Value, o)
		for _, bad := range []string{"opus-5-5", "5-5", "5.5"} {
			if strings.Contains(got, bad) {
				t.Errorf("entry %s = %q contains %q, a model brewkeg does not serve", e.Name, got, bad)
			}
		}
	}
}
