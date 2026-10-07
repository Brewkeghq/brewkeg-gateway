package brewkeg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func home(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("SHELL", "/bin/zsh")
	return h
}

// A user's config.toml is theirs. We may only change the keys we own, in the
// table we own, and never duplicate a table.
func TestCodexEditsOnlyItsOwnKeys(t *testing.T) {
	h := home(t)
	before := strings.Join([]string{
		"# my codex setup — do not touch",
		`model = "o3"`,
		`model_provider = "my-gateway"`,
		`model_reasoning_effort = "high"`,
		"",
		"[mcp_servers.filesystem]",
		`command = "npx"`,
		"",
		"[model_providers.my-gateway]",
		`name = "My Gateway"`,
		`base_url = "https://internal.corp/v1"`,
		"",
		"[model_providers.brewkeg]",
		`name = "brewkeg"`,
		`base_url = "https://old.brewkeg.dev/v1"`,
		`wire_api = "chat"`,
		`experimental_bearer_token = "bk_live_EXPIRED"`,
		`env_key = "SOMETHING"`,
		"",
		"[tui]",
		`theme = "dark"`,
		"",
	}, "\n")

	p := filepath.Join(h, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}

	b, _, err := Apply([]string{"codex"}, Options{APIKey: "bk_live_NEWKEY"})
	if err != nil {
		t.Fatal(err)
	}
	got := ReadFile(p)

	if n := strings.Count(got, "[model_providers.brewkeg]"); n != 1 {
		t.Fatalf("expected exactly one brewkeg table, found %d — duplicate tables break TOML parsing:\n%s", n, got)
	}
	for _, keep := range []string{
		"# my codex setup — do not touch",
		`model = "o3"`,
		`model_reasoning_effort = "high"`,
		"[mcp_servers.filesystem]",
		"[model_providers.my-gateway]",
		`base_url = "https://internal.corp/v1"`,
		`env_key = "SOMETHING"`,
		"[tui]",
		`theme = "dark"`,
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("user's own config was lost: %q missing from:\n%s", keep, got)
		}
	}
	for _, want := range []string{
		`base_url = "https://brewkeg.dev/v1"`,
		`wire_api = "responses"`,
		`experimental_bearer_token = "bk_live_NEWKEY"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected brewkeg to set %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "bk_live_EXPIRED") {
		t.Error("the stale brewkeg token is still present")
	}
	// The replaced provider must be visible and commented, not deleted.
	if !strings.Contains(got, `# brewkeg replaced: model_provider = "my-gateway"`) {
		t.Errorf("the previous model_provider should be commented out, not removed:\n%s", got)
	}

	if _, err := b.Restore(false); err != nil {
		t.Fatal(err)
	}
	if after := ReadFile(p); after != before {
		t.Fatalf("restore was not byte-exact:\n got:\n%s\nwant:\n%s", after, before)
	}
}

func TestCodexAddsTableWhenAbsent(t *testing.T) {
	h := home(t)
	before := "model = \"gpt-5\"\n\n[tui]\ntheme = \"dark\"\n"
	p := filepath.Join(h, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Apply([]string{"codex"}, Options{APIKey: "bk_live_K"}); err != nil {
		t.Fatal(err)
	}
	got := ReadFile(p)
	if strings.Count(got, "[model_providers.brewkeg]") != 1 {
		t.Fatalf("expected one brewkeg table:\n%s", got)
	}
	// model_provider must stay in the root scope, above the first [table].
	if strings.Index(got, "model_provider =") > strings.Index(got, "[model_providers") {
		t.Fatalf("model_provider landed inside a table, where Codex ignores it:\n%s", got)
	}
}

func TestShellRCKeepsUserLinesAndReplacesOldBlock(t *testing.T) {
	h := home(t)
	rc := filepath.Join(h, ".zshrc")
	before := strings.Join([]string{
		"export EDITOR=vim",
		`alias gs="git status"`,
		"# >>> brewkeg >>>",
		`export ANTHROPIC_BASE_URL="https://old"`,
		"# <<< brewkeg <<<",
		`alias ll="ls -la"`,
		"",
	}, "\n")
	if err := os.WriteFile(rc, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := Apply([]string{"claude-cli"}, Options{APIKey: "bk_live_K"}); err != nil {
		t.Fatal(err)
	}
	got := ReadFile(rc)

	if n := strings.Count(got, "# >>> brewkeg >>>"); n != 1 {
		t.Fatalf("expected exactly one brewkeg block, got %d:\n%s", n, got)
	}
	for _, keep := range []string{"export EDITOR=vim", `alias gs="git status"`, `alias ll="ls -la"`} {
		if !strings.Contains(got, keep) {
			t.Errorf("user's own line was dropped: %q", keep)
		}
	}
	if strings.Contains(got, "https://old") {
		t.Error("the previous brewkeg block was not replaced")
	}
}

func TestClaudeSettingsMergesIntoExistingJSON(t *testing.T) {
	h := home(t)
	before := "{\n  \"theme\": \"dark\",\n  \"env\": {\n    \"FOO\": \"bar\"\n  },\n  \"permissions\": {\n    \"allow\": [\"Bash\"]\n  }\n}\n"
	p := filepath.Join(h, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := Apply([]string{"claude-cli"}, Options{APIKey: "bk_live_K"}); err != nil {
		t.Fatal(err)
	}
	got := ReadFile(p)

	for _, keep := range []string{`"theme": "dark"`, `"FOO": "bar"`, `"allow"`} {
		if !strings.Contains(got, keep) {
			t.Errorf("settings.json lost %q:\n%s", keep, got)
		}
	}
	if !strings.Contains(got, "ANTHROPIC_AUTH_TOKEN") {
		t.Errorf("brewkeg vars were not written:\n%s", got)
	}
	if !strings.Contains(got, "bk_live_K") {
		t.Errorf("the key was not written:\n%s", got)
	}
}

// A settings.json we cannot parse must not be overwritten — refusing is the
// only safe outcome.
func TestClaudeSettingsRefusesToClobberInvalidJSON(t *testing.T) {
	h := home(t)
	p := filepath.Join(h, ".claude", "settings.json")
	broken := "{ this is not json"
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	_, results, _ := Apply([]string{"claude-cli"}, Options{APIKey: "bk_live_K"})
	if results[0].OK {
		t.Fatal("a settings.json we cannot parse must not be reported as configured")
	}
	if got := ReadFile(p); got != broken {
		t.Fatalf("the unparsable file was modified:\n got: %q\nwant: %q", got, broken)
	}
}

func TestSetTableKeysEditsOnlyNamedTable(t *testing.T) {
	in := "a = 1\n\n[t]\nx = 1\ny = 2\n\n[other]\nx = 3\n"
	got, ok := SetTableKeys(in, "t", []string{`x = 9`, `z = 4`})
	if !ok {
		t.Fatal("table t should have been found")
	}
	want := "a = 1\n\n[t]\nx = 9\ny = 2\nz = 4\n\n[other]\nx = 3\n"
	if got != want {
		t.Fatalf("unexpected edit:\n got: %q\nwant: %q", got, want)
	}
	if _, ok := SetTableKeys(in, "missing", []string{"x = 9"}); ok {
		t.Fatal("SetTableKeys must report a missing table instead of inventing one")
	}
}
