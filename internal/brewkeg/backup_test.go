package brewkeg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reformatting a backup we did not need to touch would reorder the user's JSON
// keys, so "restore" stopped being byte-exact for files brewkeg barely changed.
func TestUntouchedJSONBackupIsByteExact(t *testing.T) {
	h := home(t)
	// Keys deliberately not in alphabetical order, and no brewkeg footprint.
	original := "{\n  \"zebra\": 1,\n  \"permissions\": { \"allow\": [\"Bash\"] },\n  \"alpha\": 2\n}"
	settings := filepath.Join(h, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	b, _, err := ApplyWithSpec(DefaultSpec(), []string{"claude-cli"}, Options{APIKey: "bk_live_K"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Restore(false); err != nil {
		t.Fatal(err)
	}
	if got := ReadFile(settings); got != original {
		t.Fatalf("restore was not byte-exact:\n got: %q\nwant: %q", got, original)
	}
}

// A file that already carries our keys still gets a pre-brewkeg backup.
func TestReRunBackupStillStripsOurKeys(t *testing.T) {
	h := home(t)
	settings := filepath.Join(h, ".claude", "settings.json")
	seed := "{\n  \"theme\": \"dark\",\n  \"env\": {\n    \"ANTHROPIC_BASE_URL\": \"https://old\",\n    \"KEEP\": \"me\"\n  }\n}\n"
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	b, _, err := ApplyWithSpec(DefaultSpec(), []string{"claude-cli"}, Options{APIKey: "bk_live_K"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Restore(false); err != nil {
		t.Fatal(err)
	}
	got := ReadFile(settings)
	if strings.Contains(got, "ANTHROPIC_BASE_URL") {
		t.Errorf("restore should have removed the old brewkeg var:\n%s", got)
	}
	if !strings.Contains(got, "KEEP") || !strings.Contains(got, "dark") {
		t.Errorf("restore must keep the user's own settings:\n%s", got)
	}
}
