package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/brewkeghq/brewkeg-gateway/internal/brewkeg"
)

// Every service the window can list needs a real provider icon.
//
// The list is rendered from the spec, but the icons live in a map in app.js —
// two places, and nothing connected them. Adding OpenCode to the spec therefore
// shipped a row with an empty icon column and a label sitting flush left, next
// to four rows that had one. It reads as a layout bug, and it is: the icon is
// hidden when the map has no entry.
//
// So: read the map out of the frontend, and hold it to the spec. A target added
// without a logo fails here instead of in a screenshot.
func TestEveryTargetHasARealIcon(t *testing.T) {
	js, err := os.ReadFile(filepath.Join("frontend", "app.js"))
	if err != nil {
		t.Fatalf("reading the frontend: %v", err)
	}

	// The ICONS literal, from `const ICONS = {` to the closing `};`.
	block := regexp.MustCompile(`(?s)const ICONS = \{(.*?)\n\};`).FindStringSubmatch(string(js))
	if block == nil {
		t.Fatal("no `const ICONS = {...}` in frontend/app.js — did the map move?")
	}
	// key: "icons/file.ext" — quoted keys for the ids that are not identifiers.
	entry := regexp.MustCompile(`(?m)^\s*"?([a-z0-9-]+)"?:\s*"([^"]+)"`)
	icons := map[string]string{}
	for _, m := range entry.FindAllStringSubmatch(block[1], -1) {
		icons[m[1]] = m[2]
	}
	if len(icons) == 0 {
		t.Fatal("parsed no icons out of frontend/app.js")
	}

	for _, target := range brewkeg.Targets() {
		id := target.ID()
		path, ok := icons[id]
		if !ok {
			t.Errorf("target %q has no icon in frontend/app.js ICONS: the row renders with an empty icon column", id)
			continue
		}

		// The entry is worthless if the file is not there, or is not an image.
		rel := filepath.Join("frontend", filepath.FromSlash(path))
		data, err := os.ReadFile(rel)
		if err != nil {
			t.Errorf("target %q points at %s, which does not exist: %v", id, path, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("target %q points at %s, which is empty", id, path)
			continue
		}
		// An SVG that does not parse is a broken image, which is worse than no
		// icon: the row keeps the space and shows a torn-page glyph.
		if strings.HasSuffix(path, ".svg") {
			head := strings.TrimSpace(string(data))
			if !strings.HasPrefix(head, "<svg") || !strings.Contains(head, "<path") {
				t.Errorf("target %q points at %s, which is not a usable SVG", id, path)
			}
			if !strings.HasSuffix(head, "</svg>") {
				t.Errorf("target %q points at %s, which is a truncated SVG", id, path)
			}
		}
	}
}
