package main

// The engine moved to internal/brewkeg; these tests exercise it through that
// package so the CLI and the desktop app are both covered by the same suite.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brewkeg/brewkeg-cli/internal/brewkeg"
)

func TestBlockIdempotent(t *testing.T) {
	body := `export ANTHROPIC_BASE_URL="https://brewkeg.dev"`
	orig := "export FOO=1\n"
	once := brewkeg.ApplyBlock(orig, brewkeg.BlockBegin, brewkeg.BlockEnd, body)
	twice := brewkeg.ApplyBlock(once, brewkeg.BlockBegin, brewkeg.BlockEnd, body)

	if n := strings.Count(twice, brewkeg.BlockBegin); n != 1 {
		t.Fatalf("expected 1 block after two runs, got %d", n)
	}
	if !strings.Contains(twice, "export FOO=1") {
		t.Fatal("user line was lost")
	}
}

func TestBlockStripIsByteExact(t *testing.T) {
	orig := "model = \"gpt-5\"\nmodel_provider = \"openai\"\n\n[tui]\ntheme = \"dark\"\n"
	withBlock := brewkeg.SetRootKey(brewkeg.ApplyBlock(orig, brewkeg.BlockBegin, brewkeg.BlockEnd, "[model_providers.brewkeg]\nname = \"brewkeg\""), "model_provider", `"brewkeg"`)
	if !strings.Contains(withBlock, "# brewkeg replaced: model_provider") {
		t.Fatal("foreign model_provider was not commented out, TOML would have a duplicate key")
	}

	back, _ := brewkeg.StripBlock(withBlock)
	back = strings.Replace(back, "# brewkeg replaced: ", "", 1)
	if back != strings.TrimRight(orig, "\n")+"\n" {
		t.Fatalf("round trip changed the file:\n got: %q\nwant: %q", back, orig)
	}
}

func TestRootKeyStaysAboveFirstTable(t *testing.T) {
	in := "model = \"gpt-5\"\n\n[tui]\ntheme = \"dark\"\n"
	out := brewkeg.SetRootKey(in, "model_provider", `"brewkeg"`)

	lines := strings.Split(out, "\n")
	keyLine, tableLine := -1, -1
	for i, l := range lines {
		if strings.HasPrefix(l, "model_provider = ") && keyLine < 0 {
			keyLine = i
		}
		if strings.HasPrefix(strings.TrimSpace(l), "[") && tableLine < 0 {
			tableLine = i
		}
	}
	if keyLine < 0 || tableLine < 0 {
		t.Fatalf("missing key or table:\n%s", out)
	}
	if keyLine > tableLine {
		t.Fatal("model_provider landed inside a table — Codex would ignore it")
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	rc := filepath.Join(home, ".zshrc")
	orig := "# my shell\nexport EDITOR=vim\n"
	if err := os.WriteFile(rc, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	b := brewkeg.NewBackup("https://brewkeg.dev")
	if err := b.Capture("claude-cli", rc); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rc, []byte(brewkeg.ApplyBlock(orig, brewkeg.BlockBegin, brewkeg.BlockEnd, "export ANTHROPIC_BASE_URL=\"x\"")), 0o644); err != nil {
		t.Fatal(err)
	}

	// A second setup over the same file must not capture the first run's output.
	b2 := brewkeg.NewBackup("https://brewkeg.dev")
	if err := b2.Capture("claude-cli", rc); err != nil {
		t.Fatal(err)
	}
	if got := brewkeg.ReadFile(filepath.Join(brewkeg.BackupDir(b2.ID), b2.Entries[0].BackupFile)); got != orig {
		t.Fatalf("second backup is not the pre-brewkeg state:\n got: %q\nwant: %q", got, orig)
	}

	if _, err := b2.Restore(false); err != nil {
		t.Fatal(err)
	}
	if got := brewkeg.ReadFile(rc); got != orig {
		t.Fatalf("restore was not byte-exact:\n got: %q\nwant: %q", got, orig)
	}
}

func TestRestoreDeletesFileBrewkegCreated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	rc := filepath.Join(home, ".zshrc")

	b := brewkeg.NewBackup("https://brewkeg.dev")
	if err := b.Capture("claude-cli", rc); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rc, []byte(brewkeg.ApplyBlock("", brewkeg.BlockBegin, brewkeg.BlockEnd, "export X=1")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Restore(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rc); !os.IsNotExist(err) {
		t.Fatal("file brewkeg created should have been deleted on restore")
	}
}
