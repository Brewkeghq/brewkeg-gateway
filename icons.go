package main

import (
	"fmt"
	"os"
	"strings"
)

// The desktop app ships the real vendor artwork (icons/claude.png,
// icons/openai.svg, icons/zcode.png) and a terminal cannot show a picture.
// The CLI draws a single-width badge in the vendor's brand colour instead, so
// the checklist reads as a list of products rather than a list of boxes.
//
// The vendor comes from the server: the spec names it (icon: "anthropic",
// icon: "openai") and the CLI only decides how to spell it on a terminal. A
// target the server has not named yet falls back to the first letter of its
// label, so a new tool is never drawn as a nameless box.

type badge struct {
	glyph string
	hex   string // "rrggbb"; empty means the terminal's own foreground
}

// vendorBadges are the vendors whose brand colour is known. The colour is
// the point: the badge is the vendor's, not ours.
var vendorBadges = map[string]badge{
	"anthropic": {glyph: "Ⓐ", hex: "D97757"}, // Anthropic clay
	"openai":    {glyph: "Ⓞ", hex: "10A37F"}, // OpenAI green
}

// badgeFor picks the badge for a row. The spec's icon key wins; without one
// the label's first letter is circled, so the row still carries the product's
// own name.
func badgeFor(iconKey, label string) badge {
	if b, ok := vendorBadges[strings.ToLower(strings.TrimSpace(iconKey))]; ok {
		return b
	}
	return badge{glyph: circledInitial(label)}
}

// circledInitial turns the first letter of a label into a circled capital.
func circledInitial(label string) string {
	for _, r := range strings.TrimSpace(label) {
		switch {
		case r >= 'A' && r <= 'Z':
			return string(rune(0x24B6 + (r - 'A')))
		case r >= 'a' && r <= 'z':
			return string(rune(0x24D0 + (r - 'a')))
		default:
			return string(r)
		}
	}
	return "•"
}

// render paints the badge in the vendor's colour. Colour is skipped when the
// output is not a terminal, and NO_COLOR turns it off.
func (b badge) render() string {
	if b.hex == "" || !colorEnabled() {
		return b.glyph
	}
	r, g, bl, ok := hexRGB(b.hex)
	if !ok {
		return b.glyph
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", r, g, bl, b.glyph)
}

func hexRGB(hex string) (int, int, int, bool) {
	if len(hex) != 6 {
		return 0, 0, 0, false
	}
	v := 0
	for i := 0; i < 6; i++ {
		c := hex[i]
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'f':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int(c-'A') + 10
		default:
			return 0, 0, 0, false
		}
		v = v*16 + d
	}
	return v >> 16, (v >> 8) & 0xFF, v & 0xFF, true
}

func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
