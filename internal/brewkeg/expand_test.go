package brewkeg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A spec value that is JSON text gets its placeholders expanded inside a
// JSON string literal, so the substituted text has to be escaped for that
// literal.
//
// Windows is where this bites. The path of the binary on Windows is
// "C:\Users\me\brewkeg.exe", and "\U" is not a valid JSON escape — so the
// document stops being JSON, the writer falls back to writing the value as a
// string, and Claude Desktop gets
//
//	"managedMcpServers": "[{...}]"
//
// where it expects an array. The app then ignores the key, silently, and only
// on Windows.
//
// This is the bug that failed the v0.2.3 release: the Windows desktop build
// ran the tests and the desktop profile came out a string.
func TestExpandJSONKeepsAWindowsPathInsideAJSONString(t *testing.T) {
	winPath := `C:\Users\me\brewkeg.exe`
	doc := `[{"name":"brewkeg","transport":"stdio","command":"{{selfPath}}","args":["mcp"]}]`

	out := expandPairsJSON(doc, [][2]string{{phSelfPath, winPath}})

	var parsed []map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("the expanded value is not JSON: %v\n%s", err, out)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected one server, got %d", len(parsed))
	}
	if parsed[0]["command"] != winPath {
		t.Errorf("command = %v, want %q", parsed[0]["command"], winPath)
	}
}

// The value the writer produces is what the file holds. A string where an
// array belongs is the failure, so check the type, not just the content.
func TestTypedValueKeepsAWindowsPathAnArray(t *testing.T) {
	winPath := `C:\Users\me\brewkeg.exe`
	doc := `[{"name":"brewkeg","command":"{{selfPath}}"}]`

	value := typedValue(expandPairsJSON(doc, [][2]string{{phSelfPath, winPath}}))

	arr, ok := value.([]any)
	if !ok {
		t.Fatalf("the value must stay an array, got %T", value)
	}
	if len(arr) != 1 {
		t.Fatalf("expected one entry, got %d", len(arr))
	}
	entry := arr[0].(map[string]any)
	if entry["command"] != winPath {
		t.Errorf("command = %v, want %q", entry["command"], winPath)
	}
}

// A value that is not JSON text is expanded the plain way. A URL with a
// backslash-free value must not grow backslashes.
func TestExpandJSONLeavesAPlainValueAlone(t *testing.T) {
	out := expandPairsJSON(`"{{baseUrl}}"`, [][2]string{{phBaseURL, "https://brewkeg.dev"}})
	if out != `"https://brewkeg.dev"` {
		t.Errorf("got %s", out)
	}
}

// The guard has to be the real writer, not the helper: the helper is where
// the escaping happens, but what matters is what lands in the file. This
// writes through writeJSONPlain with a Windows path and reads the file back.
func TestDesktopProfileKeepsManagedServersAnArrayOnWindows(t *testing.T) {
	winPath := `C:\Users\me\brewkeg.exe`
	old := selfPathFn
	selfPathFn = func() string { return winPath }
	defer func() { selfPathFn = old }()

	dir := t.TempDir()
	path := filepath.Join(dir, "profile.json")
	entries := []KVSpec{{
		Name:  "managedMcpServers",
		Value: `[{"name":"brewkeg","transport":"stdio","command":"{{selfPath}}","args":["mcp"]}]`,
	}}
	if err := writeJSONPlain(path, entries, Options{}); err != nil {
		t.Fatalf("write: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the file is not valid JSON: %v\n%s", err, raw)
	}
	arr, ok := doc["managedMcpServers"].([]any)
	if !ok {
		t.Fatalf("managedMcpServers must be an array, got %T\n%s", doc["managedMcpServers"], raw)
	}
	entry := arr[0].(map[string]any)
	if entry["command"] != winPath {
		t.Errorf("command = %v, want %q", entry["command"], winPath)
	}
}
