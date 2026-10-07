package brewkeg

import (
	"regexp"
	"strings"
)

const (
	BlockBegin = "# >>> brewkeg >>>"
	BlockEnd   = "# <<< brewkeg <<<"

	// Root keys (e.g. Codex's top-level model_provider) must sit above the first
	// [table] header to be valid TOML, so they get their own marked block in
	// the root region. Two names, one stripper.
	RootBlockBegin = "# >>> brewkeg root >>>"
	RootBlockEnd   = "# <<< brewkeg root <<<"
)

var anyBlockRe = regexp.MustCompile(`(?ms)^# >>> brewkeg(?: root)? >>>\n.*?^# <<< brewkeg(?: root)? <<<\n?`)

// ApplyBlock inserts or replaces a marked block. Idempotent: running setup
// twice leaves exactly one block, and restore strips it without touching a
// single line the user wrote.
func ApplyBlock(content, begin, end, body string) string {
	full := begin + "\n" + strings.TrimRight(body, "\n") + "\n" + end + "\n"

	// Drop any previous brewkeg block first, then append: our exports must come
	// last so they win over any earlier assignment of the same variable.
	stripped := anyBlockRe.ReplaceAllString(content, "")
	if strings.TrimSpace(stripped) == "" {
		return full
	}
	return strings.TrimRight(stripped, "\n") + "\n\n" + full
}

// StripBlock removes every brewkeg block from content.
func StripBlock(content string) (string, bool) {
	if !anyBlockRe.MatchString(content) {
		return content, false
	}
	out := anyBlockRe.ReplaceAllString(content, "")
	return strings.TrimRight(out, "\n") + "\n", true
}

func HasBlock(content string) bool { return anyBlockRe.MatchString(content) }

// SetRootKey sets a top-level key in a TOML file. It cannot simply append: a
// bare key after the first [table] header belongs to that table, and Codex
// would ignore it. So the line goes into the root region, and any pre-existing
// value is commented out — writing both would be a duplicate-key parse error.
func SetRootKey(content, key, value string) string {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*=`)
	kv := key + " = " + value
	block := RootBlockBegin + "\n" + kv + "\n" + RootBlockEnd + "\n"

	lines := strings.Split(content, "\n")
	firstTable := len(lines)
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "[") {
			firstTable = i
			break
		}
	}
	head, tail := lines[:firstTable], lines[firstTable:]

	// Comment out a foreign value for this key in the root region.
	for i := range head {
		t := strings.TrimSpace(head[i])
		if re.MatchString(head[i]) && !strings.HasPrefix(t, "#") {
			head[i] = "# brewkeg replaced: " + head[i]
		}
	}

	// If a previous brewkeg root block is already here, swap its body in place
	// so we do not accumulate duplicate markers.
	replaced := false
	for i := range head {
		if strings.TrimSpace(head[i]) == strings.TrimSpace(RootBlockBegin) {
			head[i] = kv
			replaced = true
			break
		}
	}
	if !replaced {
		insertAt := 0
		for i := len(head) - 1; i >= 0; i-- {
			if strings.TrimSpace(head[i]) != "" {
				insertAt = i + 1
				break
			}
		}
		merged := append([]string{}, head[:insertAt]...)
		merged = append(merged, strings.Split(strings.TrimRight(block, "\n"), "\n")...)
		merged = append(merged, head[insertAt:]...)
		head = merged
	}

	out := strings.Join(head, "\n")
	if len(tail) > 0 {
		out += "\n" + strings.Join(tail, "\n")
	}
	return out
}
