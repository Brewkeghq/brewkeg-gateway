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

// ShellRC is the rc file we append exports to, inferred from $SHELL.
// Returns "" when the shell is unknown so we never guess and corrupt a file.
func ShellRC() string {
	shell := os.Getenv("SHELL")
	switch {
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
		return ""
	}
}

// WindowsProfile is the PowerShell profile we export into.
func WindowsProfile() string {
	dir := os.Getenv("APPDATA")
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "Microsoft", "Windows", "PowerShell", "Microsoft.PowerShell_profile.ps1")
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
func LookPath(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
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
