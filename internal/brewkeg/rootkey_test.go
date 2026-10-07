package brewkeg

import (
	"strings"
	"testing"
)

// This one shipped. A real config.toml accumulated twelve "# brewkeg replaced:"
// lines and six orphan end markers, because the old SetRootKey replaced the
// begin marker in place but never removed the end marker, and commented its own
// already-written value again on every run.
func TestSetRootKeyConvergesOnTheDamagedFile(t *testing.T) {
	damaged := `# brewkeg replaced: model_provider = "openai"
model = "gpt-5.5"
# brewkeg replaced: model_provider = "brewkeg"
# brewkeg replaced: model_provider = "brewkeg"
# <<< brewkeg root <<<
# brewkeg replaced: model_provider = "brewkeg"
# <<< brewkeg root <<<

[tui]
theme = "dark"
`

	cur := damaged
	for i := 1; i <= 6; i++ {
		cur = SetRootKey(cur, "model_provider", `"brewkeg"`)
	}

	for _, bad := range []struct{ what, sub string }{
		{"begin markers", RootBlockBegin},
		{"end markers", RootBlockEnd},
	} {
		if n := strings.Count(cur, bad.sub); n != 1 {
			t.Errorf("after repeated runs there are %d %s, want exactly 1:\n%s", n, bad.what, cur)
		}
	}
	if n := strings.Count(cur, replacedPrefix); n != 1 {
		t.Errorf("%d %q comments, want 1 — the user's own value, kept visible:\n%s", n, replacedPrefix, cur)
	}
	if !strings.Contains(cur, `model_provider = "openai"`) {
		t.Error("the user's original model_provider was discarded instead of being commented out")
	}
	if !strings.Contains(cur, "[tui]") || !strings.Contains(cur, `theme = "dark"`) {
		t.Error("tables below the root region were disturbed")
	}

	// And it must be a fixed point: running again changes nothing at all.
	again := SetRootKey(cur, "model_provider", `"brewkeg"`)
	if again != cur {
		t.Errorf("not idempotent.\nfirst:\n%s\nagain:\n%s", cur, again)
	}
}

// The ordinary case: a user's own model_provider, configured once.
func TestSetRootKeyIsIdempotentFromClean(t *testing.T) {
	clean := `model = "gpt-5.5"
model_provider = "openai"

[model_providers.openai]
name = "openai"
`
	once := SetRootKey(clean, "model_provider", `"brewkeg"`)
	twice := SetRootKey(once, "model_provider", `"brewkeg"`)

	if once != twice {
		t.Errorf("not idempotent.\nonce:\n%s\ntwice:\n%s", once, twice)
	}
	if !strings.Contains(once, `# brewkeg replaced: model_provider = "openai"`) {
		t.Error("the foreign value should stay visible, commented")
	}
	if !strings.Contains(once, "[model_providers.openai]") {
		t.Error("a table the user owns was removed")
	}
	// The block has to sit above the first table or Codex ignores the key.
	blockEnd := strings.Index(once, RootBlockEnd)
	firstTable := strings.Index(once, "[")
	if blockEnd > firstTable {
		t.Errorf("root block is below the first table:\n%s", once)
	}
}
