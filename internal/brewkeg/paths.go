// Package brewkeg is the shared engine: it knows where Claude Code, Codex and
// Claude Desktop keep their config, how to write brewkeg into those files, and
// how to put them back exactly as they were.
//
// Both front-ends use it — the CLI and the desktop app — so a change to how we
// touch a config file lands in both at once.
package brewkeg

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Version is stamped by the linker at release time.
var Version = "0.1.0"

// BaseURL is the brewkeg gateway. Overridable so forks and staging runs work.
func BaseURL() string {
	if v := os.Getenv("BREWKEG_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://brewkeg.dev"
}

func Home() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	if runtime.GOOS == "windows" {
		return os.Getenv("USERPROFILE")
	}
	return os.Getenv("HOME")
}

func HomeJoin(parts ...string) string {
	return filepath.Join(append([]string{Home()}, parts...)...)
}

// Home_ is the CLI-facing alias; the engine keeps its own home resolution.
func brewkegHome() string { return HomeJoin(".brewkeg") }

func BackupsRoot() string { return filepath.Join(brewkegHome(), "backups") }

// EnsurePrivateDir makes dir exist and owner-only.
//
// MkdirAll does not tighten a directory that already exists, and everything
// under ~/.brewkeg is a copy of, or is, a credential: the key store and every
// backup of a tool config that holds ANTHROPIC_AUTH_TOKEN. So this both creates
// with 0700 and chmods an existing one.
func EnsurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil && !os.IsPermission(err) {
		return err
	}
	return nil
}

// rcCandidates are the rc files we are willing to write exports into, most
// likely first. They double as the fallback for detection.
func rcCandidates() []string {
	return []string{
		HomeJoin(".zshrc"),
		HomeJoin(".bashrc"),
		HomeJoin(".bash_profile"),
		HomeJoin(".config", "fish", "config.fish"),
		HomeJoin(".config", "nushell", "config.nu"),
	}
}

// ShellRC is the rc file we append exports to, inferred from $SHELL.
//
// $SHELL is empty when the desktop app is launched from Finder or Spotlight:
// macOS starts GUI apps with no login environment. Falling back to "" there
// would mean the app silently never sees (or writes) the user's shell config,
// so when the shell is unknown we use whichever rc file actually exists.
func ShellRC() string {
	// Windows has no rc files in this list, and a GUI launch has no $SHELL at
	// all, so the fallback below would return "" and the block would be
	// silently skipped — a Claude Code CLI user would get their settings.json
	// env but no exports, and the tool would look configured in one place and
	// not the other. PowerShell has exactly one profile; use it.
	if isWindows() {
		return WindowsProfile()
	}
	switch shell := os.Getenv("SHELL"); {
	case strings.Contains(shell, "zsh"):
		return HomeJoin(".zshrc")
	case strings.Contains(shell, "bash"):
		if FileExists(HomeJoin(".bashrc")) {
			return HomeJoin(".bashrc")
		}
		return HomeJoin(".bash_profile")
	case strings.Contains(shell, "fish"):
		return HomeJoin(".config", "fish", "config.fish")
	case strings.Contains(shell, "nu"):
		return HomeJoin(".config", "nushell", "config.nu")
	default:
		for _, rc := range rcCandidates() {
			if FileExists(rc) {
				return rc
			}
		}
		return ""
	}
}

// ShellRCsWithBrewkegBlock lists every known rc file carrying our marked block.
// Detection must not depend on ShellRC() alone: an app launched without
// $SHELL, or a user who switched shells after setup, would otherwise show the
// switch as off even though their config is being pointed at brewkeg right now.
func ShellRCsWithBrewkegBlock() []string {
	var hits []string
	for _, rc := range rcCandidates() {
		if HasBlock(ReadFile(rc)) {
			hits = append(hits, rc)
		}
	}
	return hits
}

// WindowsProfile is the PowerShell profile we export into.
func WindowsProfile() string {
	dir := os.Getenv("APPDATA")
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "Microsoft", "Windows", "PowerShell", "Microsoft.PowerShell_profile.ps1")
}

func isWindows() bool { return runtime.GOOS == "windows" }

// DirExists reports whether p is a directory. Needed because the tool configs
// live in directories (~/.claude, ~/.codex) and FileExists deliberately rejects
// directories — checking them with it silently reports "not installed".
func DirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func FileExists(p string) bool {
	if p == "" {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func ReadFile(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

func WriteFile(p, content string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(content), 0o644)
}

// LookPath reports whether a binary is on PATH, used only for detection hints.
// A Finder-launched app inherits a minimal PATH (no Homebrew, no ~/.local/bin),
// so we also check the usual install locations before calling a tool missing.
func LookPath(bin string) bool {
	if _, err := exec.LookPath(bin); err == nil {
		return true
	}
	for _, dir := range []string{
		"/opt/homebrew/bin", "/usr/local/bin",
		HomeJoin(".local", "bin"), HomeJoin("bin"),
		HomeJoin(".bun", "bin"), "/usr/bin",
	} {
		if FileExists(filepath.Join(dir, bin)) {
			return true
		}
	}
	return false
}

// MaskKey is how a key is shown back to a human: enough to recognise, not enough
// to use.
func MaskKey(k string) string {
	if k == "" {
		return ""
	}
	if len(k) <= 8 {
		return strings.Repeat("*", len(k))
	}
	return k[:6] + strings.Repeat("*", 6) + k[len(k)-2:]
}

// RepoURL is where the CLI and desktop app source lives. Shown in the app so
// anyone can read exactly what touches their machine.
const RepoURL = "https://github.com/brewkeghq/brewkeg-gateway"
