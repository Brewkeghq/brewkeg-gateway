package brewkeg

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Windows has no POSIX mode bits. Go's os.Stat reports 0666 for every file and
// 0777 for every directory there, so a 0600/0700 assertion is not a weaker
// check on Windows — it is a check the platform cannot satisfy at all. The
// chmod calls in the engine are still made (harmless there, load-bearing the
// moment the store lives on a POSIX filesystem), but the tests must not assert
// an outcome Windows cannot produce.
func requirePOSIXPerms(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits do not exist on Windows")
	}
}

// seedShellRC writes seed into the rc file ShellRC() will actually choose here,
// so a test that means "an rc file that already exists" seeds the file the
// writer reads. Hardcoding ~/.zshrc made a Windows run seed a POSIX file the
// resolver never returns, and the assertions then failed for a reason that had
// nothing to do with the code under test.
func seedShellRC(t *testing.T, seed string) string {
	t.Helper()
	rc := ShellRC()
	if rc == "" {
		t.Skipf("ShellRC() resolved to nothing on %s; no rc file to seed", runtime.GOOS)
	}
	if err := os.MkdirAll(filepath.Dir(rc), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rc, err)
	}
	if err := os.WriteFile(rc, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed %s: %v", rc, err)
	}
	return rc
}