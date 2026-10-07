package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/brewkeg/brewkeg-cli/internal/brewkeg"
)

// UpdateInfo is what the window shows in the update banner.
type UpdateInfo struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`
	Notes     string `json:"notes,omitempty"`
}

const releasesAPI = "https://api.github.com/repos/brewkeg/brewkeg-cli/releases/latest"

// CheckForUpdate asks GitHub for the newest release and compares it with the
// running binary. A notifier, not a silent self-install: the app never swaps
// its own executable behind your back, it just tells you and opens the page.
func (a *App) CheckForUpdate() UpdateInfo {
	info := UpdateInfo{Current: currentVersion(), URL: releasePage()}

	ctx, cancel := context.WithTimeout(a.context(), 6*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPI, nil)
	if err != nil {
		return info
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "brewkeg-desktop")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return info
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return info
	}

	var rel struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(res.Body).Decode(&rel); err != nil {
		return info
	}

	latest := strings.TrimPrefix(rel.TagName, "cli-v")
	info.Latest = latest
	if rel.HTMLURL != "" {
		info.URL = rel.HTMLURL
	}
	info.Available = newerVersion(latest, info.Current)
	if info.Available {
		info.Notes = firstLine(rel.Body)
	}
	return info
}

// OpenUpdate takes the user straight to the release, which is where the
// installer for their platform is.
func (a *App) OpenUpdate(url string) {
	if url == "" {
		url = releasePage()
	}
	runtime.BrowserOpenURL(a.ctx, url)
}

func releasePage() string { return "https://github.com/brewkeg/brewkeg-cli/releases/latest" }

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

// newerVersion reports whether candidate is a later release than current. Both
// may carry a leading "v"; an unparsable version is never treated as newer, so a
// malformed tag cannot nag a user into a downgrade.
func newerVersion(candidate, current string) bool {
	if strings.TrimSpace(candidate) == "" || strings.TrimSpace(current) == "" {
		return false
	}
	c := parseVersion(candidate)
	n := parseVersion(current)
	for i := 0; i < 3; i++ {
		if c[i] != n[i] {
			return c[i] > n[i]
		}
	}
	return false
}

func parseVersion(v string) [3]int {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(v, "cli-v"), "v"))
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, part := range strings.SplitN(v, ".", 3) {
		if i > 2 {
			break
		}
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return [3]int{0, 0, 0}
		}
		out[i] = n
	}
	if v == "" {
		return [3]int{0, 0, 0}
	}
	return out
}

func (a *App) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func currentVersion() string { return brewkeg.Version }
