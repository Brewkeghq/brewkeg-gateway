package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/brewkeghq/brewkeg-gateway/internal/brewkeg"
)

const releasesAPI = "https://api.github.com/repos/brewkeghq/brewkeg-gateway/releases/latest"

type release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// runUpdate checks for, and installs, a newer brewkeg.
func runUpdate(args []string) int {
	check := hasFlag(args, "--check")

	rel, err := latestRelease()
	if err != nil {
		fmt.Fprintf(os.Stderr, "brewkeg: %v\n", err)
		return 1
	}
	latest := strings.TrimPrefix(rel.TagName, "v")

	if latest == brewkeg.Version {
		fmt.Printf("brewkeg %s is already the latest release\n", brewkeg.Version)
		return 0
	}
	if check {
		fmt.Printf("brewkeg %s → latest release %s\n", brewkeg.Version, latest)
		return 0
	}
	if !newerVersion(latest, brewkeg.Version) {
		fmt.Printf("brewkeg %s is newer than the latest release %s — nothing to do\n", brewkeg.Version, latest)
		return 0
	}

	asset, ok := cliAsset(rel)
	if !ok {
		fmt.Fprintf(os.Stderr, "brewkeg: release %s has no binary for %s/%s\n", latest, runtime.GOOS, runtime.GOARCH)
		return 1
	}

	fmt.Printf("Downloading brewkeg %s…\n", latest)
	body, err := download(asset.URL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brewkeg: %v\n", err)
		return 1
	}
	if strings.HasSuffix(asset.Name, ".zip") {
		body, err = binaryFromZip(body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "brewkeg: %v\n", err)
			return 1
		}
	}

	sums, err := checksums(rel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brewkeg: %v\n", err)
		return 1
	}
	want, ok := sums[asset.Name]
	if !ok {
		fmt.Fprintf(os.Stderr, "brewkeg: release %s has no checksum for %s\n", latest, asset.Name)
		return 1
	}
	got := sha256.Sum256(body)
	if hex.EncodeToString(got[:]) != want {
		fmt.Fprintln(os.Stderr, "brewkeg: checksum mismatch — nothing was installed")
		return 1
	}

	target, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "brewkeg: %v\n", err)
		return 1
	}
	if err := os.WriteFile(target, body, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "brewkeg: could not replace %s: %v\n", target, err)
		return 1
	}
	fmt.Printf("✔ brewkeg %s installed to %s\n", latest, target)
	return 0
}

// cliAsset finds the published binary for this machine. The name is read from
// the release, never derived: deriving it is what broke every install.
func cliAsset(rel release) (struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}, bool) {
	suffix := "_" + runtime.GOOS + "_" + runtime.GOARCH
	for _, a := range rel.Assets {
		if !strings.HasPrefix(a.Name, "brewkeg_") {
			continue
		}
		name := strings.TrimSuffix(a.Name, ".zip")
		if strings.HasSuffix(name, suffix) {
			return a, true
		}
	}
	return struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	}{}, false
}

// checksums reads the release's checksum file.
func checksums(rel release) (map[string]string, error) {
	for _, a := range rel.Assets {
		if !strings.Contains(a.Name, "checksums") {
			continue
		}
		body, err := download(a.URL)
		if err != nil {
			return nil, err
		}
		out := map[string]string{}
		for _, line := range strings.Split(string(body), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			out[strings.TrimPrefix(fields[1], "*")] = fields[0]
		}
		return out, nil
	}
	return nil, fmt.Errorf("release has no checksum file")
}

// binaryFromZip pulls the executable out of a release archive.
func binaryFromZip(body []byte) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("could not read the archive: %w", err)
	}
	name := "brewkeg"
	if runtime.GOOS == "windows" {
		name = "brewkeg.exe"
	}
	for _, f := range r.File {
		if filepath.Base(f.Name) != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, fmt.Errorf("archive has no %s", name)
}

// latestRelease asks GitHub for the newest published release.
func latestRelease() (release, error) {
	body, err := brewkeg.FetchJSON(context.Background(), releasesAPI)
	if err != nil {
		return release{}, fmt.Errorf("could not reach GitHub: %w", err)
	}
	var rel release
	if err := json.Unmarshal(body, &rel); err != nil {
		return release{}, fmt.Errorf("could not read the release list")
	}
	if rel.TagName == "" {
		return release{}, fmt.Errorf("no release found")
	}
	return rel, nil
}

// download fetches a URL.
func download(url string) ([]byte, error) {
	return brewkeg.FetchJSON(context.Background(), url)
}

// newerVersion reports whether candidate is later than current.
func newerVersion(candidate, current string) bool {
	c := parseVersion(candidate)
	n := parseVersion(current)
	for i := range c {
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
		n := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				n = -1
				break
			}
			n = n*10 + int(r-'0')
		}
		if n < 0 {
			return [3]int{0, 0, 0}
		}
		out[i] = n
	}
	return out
}
