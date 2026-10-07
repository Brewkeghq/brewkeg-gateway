package main

import "testing"

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		candidate, current string
		want               bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.1.1", "0.1.0", true},
		{"1.0.0", "0.9.9", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", false},
		{"0.0.9", "0.1.0", false},
		{"0.2.0-rc1", "0.1.0", true},
		{"", "0.1.0", false},
		{"0.1.0", "", false},
		{"not-a-version", "0.1.0", false},
		{"0.3", "0.1.0", true},
	}
	for _, c := range cases {
		if got := newerVersion(c.candidate, c.current); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.candidate, c.current, got, c.want)
		}
	}
}

func TestParseVersionStripsReleasePrefixes(t *testing.T) {
	for _, in := range []string{"cli-v0.2.0", "v0.2.0", "0.2.0"} {
		got := parseVersion(in)
		if got != [3]int{0, 2, 0} {
			t.Errorf("parseVersion(%q) = %v", in, got)
		}
	}
}

func TestFirstLineSkipsBlanks(t *testing.T) {
	if got := firstLine("\n\n  Adds OpenCode  \nmore text"); got != "Adds OpenCode" {
		t.Errorf("firstLine = %q", got)
	}
	if got := firstLine("   \n\n"); got != "" {
		t.Errorf("firstLine of whitespace = %q", got)
	}
}

// The notifier must never break setup: a bad or empty response is simply "no
// update", not an error the user has to deal with.
func TestCheckForUpdateNeverPanics(t *testing.T) {
	app := NewApp()
	info := app.CheckForUpdate()
	if info.URL == "" {
		t.Error("update info should always carry a URL to fall back on")
	}
	if info.Current == "" {
		t.Error("update info should always report the running version")
	}
}
