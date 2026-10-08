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
	home(t) // seeds a temp HOME; the rc file comes from the resolver below.
	before := strings.Join([]string{
		"export EDITOR=vim",
		`alias gs="git status"`,
		"# >>> brewkeg >>>",
		`export ANTHROPIC_BASE_URL="https://old"`,
		"# <<< brewkeg <<<",
		`alias ll="ls -la"`,
		"",
	}, "\n")
	rc := seedShellRC(t, before)

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

// The desktop app is launched from Finder, which gives it no $SHELL and a
// minimal PATH. Detection that only works in a login shell shows every tool as
// "not installed" in the one place it matters most.
func TestStatusAllWorksWithoutShellEnv(t *testing.T) {
	h := home(t)
	t.Setenv("SHELL", "")
	t.Setenv("PATH", "/usr/bin:/bin")

	if err := os.MkdirAll(filepath.Join(h, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(h, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Already configured by a previous run, exactly as a returning user is.
	toml := "model_provider = \"brewkeg\"\n\n[model_providers.brewkeg]\nbase_url = \"https://brewkeg.dev/v1\"\n"
	if err := os.WriteFile(filepath.Join(h, ".codex", "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h, ".zshrc"), []byte("export EDITOR=vim\n"+BlockBegin+"\nexport ANTHROPIC_BASE_URL=\"https://brewkeg.dev\"\n"+BlockEnd+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := map[string]TargetStatus{}
	for _, s := range StatusAll() {
		got[s.ID] = s
	}

	if !got["claude-cli"].Installed {
		t.Error("~/.claude is a directory; treating it as a missing file reports the tool as not installed")
	}
	if !got["codex"].Installed {
		t.Error("~/.codex is a directory; it should count as installed")
	}
	if !got["claude-cli"].Enabled {
		t.Error("a brewkeg block in .zshrc must show the switch as on, even with no $SHELL")
	}
	if !got["codex"].Enabled {
		t.Error("an existing [model_providers.brewkeg] table must show the switch as on")
	}
}

// ShellRC must fall back to an rc file that exists, so a GUI-launched app still
// finds the right place to write exports. Windows has no rc files in the
// candidate list at all — it resolves to the PowerShell profile, which
// paths_test.go covers on a Windows runner.
func TestShellRCFallsBackToExistingFile(t *testing.T) {
	if isWindows() {
		t.Skip("no POSIX rc files on Windows; see TestWindowsShellBlockGoesToThePowerShellProfile")
	}
	h := home(t)
	t.Setenv("SHELL", "")
	if err := os.WriteFile(filepath.Join(h, ".bashrc"), []byte("# bash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ShellRC(); got != filepath.Join(h, ".bashrc") {
		t.Errorf("ShellRC() = %q, want the existing .bashrc", got)
	}
	t.Setenv("SHELL", "/bin/zsh")
	if got := ShellRC(); got != filepath.Join(h, ".zshrc") {
		t.Errorf("with SHELL set, ShellRC() = %q, want ~/.zshrc", got)
	}
}

// Claude Code caches the model list per base URL. Point it at a new gateway and
// keep the old cache, and the picker keeps advertising models the new gateway
// does not serve.
func TestStaleGatewayCacheIsCleared(t *testing.T) {
	h := home(t)
	cache := filepath.Join(h, ".claude", "cache", "gateway-models.json")
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, []byte(`{"baseUrl":"https://brewkeg.dev"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	b, _, err := Apply([]string{"claude-cli"}, Options{APIKey: "bk_live_K"})
	if err != nil {
		t.Fatal(err)
	}
	if FileExists(cache) {
		t.Fatal("the stale gateway cache should be cleared when we change the base url")
	}
	var seen bool
	for _, e := range b.Entries {
		if e.Path == cache {
			seen = true
			if !e.Transient {
				t.Error("the cache entry must be transient, or a restore would resurrect it")
			}
			if !FileExists(filepath.Join(BackupDir(b.ID), e.BackupFile)) {
				t.Error("the cache copy should still be kept in the backup")
			}
		}
	}
	if !seen {
		t.Fatal("the cleared cache was not recorded in the backup manifest")
	}

	// Restoring must not put the old gateway's model list back.
	if _, err := b.Restore(false); err != nil {
		t.Fatal(err)
	}
	if FileExists(cache) {
		t.Error("restore resurrected the stale cache; it should be left cleared")
	}
}

func TestNoCacheToClearIsNotAnError(t *testing.T) {
	home(t)
	if n := len(StaleGatewayCachesPending()); n != 0 {
		t.Fatalf("fresh machine should have no pending caches, got %d", n)
	}
	if _, _, err := Apply([]string{"claude-cli"}, Options{APIKey: "bk_live_K"}); err != nil {
		t.Fatalf("a machine with no cache must still configure cleanly: %v", err)
	}
}

// A user who closes the window and sees no change assumes we broke something,
// so naming what to reopen is part of the job, not a nicety.
func TestRestartHintsNameEveryChangedThing(t *testing.T) {
	hints := RestartHintsFor(
		[]string{"claude-cli", "codex"},
		[]string{"~/.claude/settings.json", "~/.zshrc", "~/.codex/config.toml"},
	)
	want := map[string]string{
		"Claude Code":   "quit and reopen",
		"Codex CLI":     "quit and reopen",
		"Your terminal": "open a new window",
	}
	got := map[string]string{}
	for _, h := range hints {
		got[h.What] = h.Action
	}
	for what, action := range want {
		if got[what] != action {
			t.Errorf("missing or wrong hint for %q: got %q want %q (all: %+v)", what, got[what], action, hints)
		}
	}
}

// Writing only to settings.json means no shell restart is needed — telling a
// user to open a new terminal for nothing trains them to ignore the panel.
func TestNoShellRestartWithoutShellEdits(t *testing.T) {
	hints := RestartHintsFor([]string{"codex"}, []string{"~/.codex/config.toml"})
	for _, h := range hints {
		if h.What == "Your terminal" {
			t.Fatalf("shell was not touched, so no terminal hint: %+v", hints)
		}
	}
}

// The reverse: an rc file was written, so the terminal must be named.
func TestTerminalHintOnlyWhenShellTouched(t *testing.T) {
	hints := RestartHintsFor([]string{"claude-cli"}, []string{"~/.claude/settings.json", "~/.config/fish/config.fish"})
	found := false
	for _, h := range hints {
		if h.What == "Your terminal" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a shell rc was written, so the terminal hint is required: %+v", hints)
	}
}
