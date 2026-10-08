package brewkeg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The app bootstraps this directory itself on first access. If our writer
// produces anything the app would not produce, the app ignores it — and a user
// sees "configured" over a connection that does not exist. So this asserts the
// literal shape, not merely that a file appeared.
func TestEnsureProfileCreatesTheShapeTheAppCreates(t *testing.T) {
	withTempHome(t)

	p, err := ClaudeDesktopEnsureProfile()
	if err != nil {
		t.Fatal(err)
	}
	dir := ClaudeDesktopConfigDir()

	base := filepath.Base(p)
	if filepath.Ext(base) != ".json" || base == "_meta.json" {
		t.Fatalf("applied config is not a <id>.json, got %q", base)
	}
	// The app guards every id with /^[a-f0-9-]{36}$/ before it will read the
	// file. An id that fails this is silently ignored.
	id := strings.TrimSuffix(base, ".json")
	if !configID.MatchString(id) {
		t.Fatalf("id %q does not match the app's ^[a-f0-9-]{36}$ guard", id)
	}

	var meta struct {
		AppliedID string `json:"appliedId"`
		Entries   []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"entries"`
	}
	raw := ReadFile(filepath.Join(dir, "_meta.json"))
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		t.Fatalf("_meta.json is not valid JSON: %v\n%s", err, raw)
	}
	if meta.AppliedID != id {
		t.Fatalf("appliedId %q does not match the file we wrote %q", meta.AppliedID, id)
	}
	if len(meta.Entries) != 1 || meta.Entries[0].ID != id || meta.Entries[0].Name == "" {
		t.Fatalf("entries must name the applied config: %+v", meta.Entries)
	}

	// And the applied file must now resolve the way the app would read it.
	if got := ClaudeDesktopAppliedConfig(); got != p {
		t.Fatalf("AppliedConfig = %q, want %q", got, p)
	}
}

// Creating a profile the user already has would repoint an app they had
// configured on purpose.
func TestEnsureProfileAdoptsAnExistingOneInsteadOfReplacingIt(t *testing.T) {
	withTempHome(t)

	first, err := ClaudeDesktopEnsureProfile()
	if err != nil {
		t.Fatal(err)
	}
	// The user edits their own profile.
	if err := os.WriteFile(first, []byte(`{"inferenceModels":["mine"]}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	second, err := ClaudeDesktopEnsureProfile()
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("a second run minted a new profile: %q -> %q", first, second)
	}
	if got := ReadFile(first); !strings.Contains(got, "mine") {
		t.Fatalf("the user's own profile was overwritten:\n%s", got)
	}
}

// Two runs must not collide: the id comes from crypto/rand, and a collision
// would mean two callers writing the same file.
func TestEnsureProfileMintsDistinctIDs(t *testing.T) {
	withTempHome(t)
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		id, err := newConfigID()
		if err != nil {
			t.Fatal(err)
		}
		if !configID.MatchString(id) {
			t.Fatalf("minted id %q fails the app's guard", id)
		}
		if seen[id] {
			t.Fatalf("minted a duplicate id: %s", id)
		}
		seen[id] = true
	}
}

// A hand-edited _meta.json with an id the app would reject must not send us
// writing to a file the app never reads.
func TestAppliedConfigRejectsAnIDTheAppWouldIgnore(t *testing.T) {
	withTempHome(t)
	dir := ClaudeDesktopConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"Default", "NOT-A-UUID", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaaaa"} {
		if err := os.WriteFile(filepath.Join(dir, bad+".json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		meta, _ := json.Marshal(map[string]any{"appliedId": bad, "entries": []any{map[string]any{"id": bad, "name": "x"}}})
		if err := os.WriteFile(filepath.Join(dir, "_meta.json"), meta, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := ClaudeDesktopAppliedConfig(); got != "" {
			t.Fatalf("accepted id %q, which the app's own guard would reject (%q)", bad, got)
		}
	}
}

// Evicting must never bring a profile into existence just to strip the keys back
// out of it.
func TestEvictDoesNotCreateAProfile(t *testing.T) {
	withTempHome(t)
	if _, _, err := ApplyWithSpec(DefaultSpec(), nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if FileExists(filepath.Join(ClaudeDesktopConfigDir(), "_meta.json")) {
		t.Fatal("eviction created a configuration library the user never had")
	}
}
