package brewkeg

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A GUI-launched app on Windows has no $SHELL and no rc file to fall back to,
// so ShellRC returned "" and the export block was silently skipped. Only a
// Windows runner can prove the fix; the skip keeps the rest of the suite
// meaningful on macOS and Linux.
func TestWindowsShellBlockGoesToThePowerShellProfile(t *testing.T) {
	if !isWindows() {
		t.Skip("PowerShell profile is a Windows-only path")
	}
	appData := t.TempDir()
	t.Setenv("APPDATA", appData)
	t.Setenv("SHELL", "")

	got := ShellRC()
	want := filepath.Join(appData, "Microsoft", "Windows", "PowerShell", "Microsoft.PowerShell_profile.ps1")
	if got != want {
		t.Fatalf("ShellRC() = %q, want %q", got, want)
	}
	if err := WriteFile(got, ""); err != nil {
		t.Fatal(err)
	}
	if err := writeShellBlock(got, claudeEnvEntries(), Options{APIKey: "bk_live_K", BaseURL: "https://brewkeg.test"}); err != nil {
		t.Fatal(err)
	}
	body := ReadFile(got)
	if !strings.Contains(body, "$env:ANTHROPIC_AUTH_TOKEN") {
		t.Fatalf("PowerShell profile did not get $env: syntax:\n%s", body)
	}
	if strings.Contains(body, "export ANTHROPIC_AUTH_TOKEN") {
		t.Fatalf("sh syntax written to a PowerShell profile:\n%s", body)
	}
}

// The CI failure this file exists to prevent was platform-shaped: four tests
// built a macOS path by hand while the code resolved per-OS, so they were green
// on a Mac and red on the Linux runner. Asserting on a hardcoded literal is the
// same mistake with the serial numbers filed off.
//
// These assertions are written against runtime.GOOS, so they only ever check the
// platform actually running them — and they run on every one.
func TestEveryPerOSMapCoversTheRunningPlatform(t *testing.T) {
	s := DefaultSpec()
	for _, ts := range s.Targets {
		for i, f := range ts.Files {
			if len(f.Paths) == 0 {
				continue
			}
			if _, ok := f.Paths[runtime.GOOS]; !ok {
				t.Errorf("%s file %d (%s) has no path for %s — it would be silently skipped",
					ts.ID, i, f.Kind, runtime.GOOS)
			}
		}
		for os, d := range ts.Detect.DirsByOS {
			if len(d) == 0 {
				t.Errorf("%s detect dirs for %s is empty", ts.ID, os)
			}
		}
		if len(ts.Detect.DirsByOS) > 0 {
			if _, ok := ts.Detect.DirsByOS[runtime.GOOS]; !ok {
				t.Errorf("%s detects by OS but has no dirs for %s — it would report not installed",
					ts.ID, runtime.GOOS)
			}
		}
	}
}

// A relative path in a spec is joined to $HOME. An absolute one is a bug that
// only bites the platform it was written for — /Library/… on macOS, C:\ on
// Windows — so no path may start at the filesystem root.
func TestSpecPathsAreRelativeToHome(t *testing.T) {
	for _, ts := range DefaultSpec().Targets {
		for i, f := range ts.Files {
			for os, p := range f.Paths {
				if p == "" {
					t.Errorf("%s file %d: empty path for %s", ts.ID, i, os)
					continue
				}
				if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
					t.Errorf("%s file %d: %q is absolute for %s; paths must be relative to $HOME",
						ts.ID, i, p, os)
				}
			}
		}
	}
}

// The Claude Desktop layout is the one place with two conventions in a single
// app, and getting it wrong writes to a directory the app never reads. The
// spec and the detector must therefore agree, on every platform.
func TestDeveloperSettingsPathMatchesWhatWeRead(t *testing.T) {
	want := ClaudeDesktopDeveloperSettings()
	if !strings.HasSuffix(want, filepath.Join("Claude", "developer_settings.json")) {
		t.Fatalf("developer_settings.json must live in plain userData, got %q", want)
	}
	if strings.Contains(want, "Claude-3p") {
		t.Fatalf("developer_settings.json is NOT written to the -3p directory: %q", want)
	}

	for _, ts := range DefaultSpec().Targets {
		if ts.ID != "desktop" {
			continue
		}
		found := false
		for _, f := range ts.Files {
			if f.Kind != "json-plain" {
				continue
			}
			// Only this platform's entry can be compared with this platform's
			// resolver; the other keys are for machines we are not running on.
			if p, ok := f.Paths[runtime.GOOS]; ok && strings.HasSuffix(p, "developer_settings.json") {
				found = true
				if got := resolvePath(p); got != want {
					t.Fatalf("spec writes %q but the detector reads %q", got, want)
				}
			}
		}
		if !found {
			t.Fatal("the desktop target no longer writes developer_settings.json")
		}
	}
}
