package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlockIdempotent(t *testing.T) {
	body := `export ANTHROPIC_BASE_URL="https://brewkeg.dev"`
	orig := "export FOO=1\n"
	once := applyBlock(orig, blockBegin, blockEnd, body)
	twice := applyBlock(once, blockBegin, blockEnd, body)

	if n := strings.Count(twice, blockBegin); n != 1 {
		t.Fatalf("expected 1 block after two runs, got %d", n)
	}
	if !strings.Contains(twice, "export FOO=1") {
		t.Fatal("user line was lost")
	}
}

func TestBlockStripIsByteExact(t *testing.T) {
	orig := "model = \"gpt-5\"\nmodel_provider = \"openai\"\n\n[tui]\ntheme = \"dark\"\n"
	withBlock := setRootKey(applyBlock(orig, blockBegin, blockEnd, "[model_providers.brewkeg]\nname = \"brewkeg\""), "model_provider", `"brewkeg"`)
	if !strings.Contains(withBlock, "# brewkeg replaced: model_provider") {
		t.Fatal("foreign model_provider was not commented out, TOML would have a duplicate key")
	}

	back, _ := stripBlock(withBlock)
	back = strings.Replace(back, "# brewkeg replaced: ", "", 1)
	if back != strings.TrimRight(orig, "\n")+"\n" {
		t.Fatalf("round trip changed the file:\n got: %q\nwant: %q", back, orig)
	}
}

func TestRootKeyStaysAboveFirstTable(t *testing.T) {
	in := "model = \"gpt-5\"\n\n[tui]\ntheme = \"dark\"\n"
	out := setRootKey(in, "model_provider", `"brewkeg"`)

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

	b := &Backup{ID: "test", CreatedAt: nowISO()}
	if err := b.capture("claude-cli", rc); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rc, []byte(applyBlock(orig, blockBegin, blockEnd, "export ANTHROPIC_BASE_URL=\"x\"")), 0o644); err != nil {
		t.Fatal(err)
	}

	// A second setup over the same file must not capture the first run's output.
	b2 := &Backup{ID: "test2", CreatedAt: nowISO()}
	if err := b2.capture("claude-cli", rc); err != nil {
		t.Fatal(err)
	}
	if got := readFile(filepath.Join(backupDir("test2"), sanitize(rc))); got != orig {
		t.Fatalf("second backup is not the pre-brewkeg state:\n got: %q\nwant: %q", got, orig)
	}

	if _, err := b2.restore(false); err != nil {
		t.Fatal(err)
	}
	if got := readFile(rc); got != orig {
		t.Fatalf("restore was not byte-exact:\n got: %q\nwant: %q", got, orig)
	}
}

func TestRestoreDeletesFileBrewkegCreated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	rc := filepath.Join(home, ".zshrc")

	b := &Backup{ID: "test", CreatedAt: nowISO()}
	if err := b.capture("claude-cli", rc); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rc, []byte(applyBlock("", blockBegin, blockEnd, "export X=1")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := b.restore(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rc); !os.IsNotExist(err) {
		t.Fatal("file brewkeg created should have been deleted on restore")
	}
}
