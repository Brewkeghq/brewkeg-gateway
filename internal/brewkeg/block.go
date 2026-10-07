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

// SetTableKeys edits one [table] in place: it replaces the values of the keys we
// own and adds only the ones that are missing, leaving every other line exactly
// as the user wrote it. Appending a second table of the same name would be a
// duplicate-key parse error, so this is the only safe way to touch a table the
// user may already have.
func SetTableKeys(content, table string, kv []string) (string, bool) {
	lines := strings.Split(content, "\n")

	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "["+table+"]" {
			start = i
			break
		}
	}
	if start < 0 {
		return content, false
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}

	for _, pair := range kv {
		key := strings.TrimSpace(strings.SplitN(pair, "=", 2)[0])
		re := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)
		found := false
		for i := start + 1; i < end; i++ {
			t := strings.TrimSpace(lines[i])
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			if re.MatchString(lines[i]) {
				lines[i] = pair
				found = true
				break
			}
		}
		if !found {
			// append at the end of this table, before any trailing comments
			ins := end
			for ins > start+1 && strings.TrimSpace(lines[ins-1]) == "" {
				ins--
			}
			merged := append([]string{}, lines[:ins]...)
			merged = append(merged, pair)
			lines = append(merged, lines[ins:]...)
			end++
		}
	}
	return strings.Join(lines, "\n"), true
}

// TableHas reports whether a [table] exists.
func TableHas(content, table string) bool {
	return strings.Contains(content, "\n["+table+"]") || strings.HasPrefix(content, "["+table+"]")
}

// CodexPointsAtBrewkeg reports whether a codex config.toml routes through
// brewkeg — the provider table plus the root key that selects it.
//
// It deliberately does not look for our own marker: plenty of people wire
// brewkeg in by hand, and the switch in the app has to agree with what Codex
// actually does, not with how the file got that way.
func CodexPointsAtBrewkeg(content string) bool {
	return TableHas(content, "model_providers.brewkeg") && RootKeyIs(content, "model_provider", "brewkeg")
}

// RootKeyIs reports whether a root-scope key has the given value. Root scope
// only — a `model_provider` inside any [table] belongs to that table and is
// not what Codex reads.
func RootKeyIs(content, key, want string) bool {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*=\s*"([^"]*)"`)
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			break
		}
		if m := re.FindStringSubmatch(line); m != nil {
			return m[1] == want
		}
	}
	return false
}
