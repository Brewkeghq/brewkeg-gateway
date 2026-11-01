package main

import (
	"fmt"
	"math"
	"os"
	"strings"
)

// The terminal cannot show the vendor's artwork. The desktop app ships the real
// files (icons/claude.png, icons/openai.svg, icons/opencode.svg, icons/zcode.png)
// and draws them; a checklist row here is one text cell wide.
//
// So each row carries the vendor's MARK, in the vendor's colour, drawn with the
// closest character that IS that shape — not the first letter of the product's
// name. A circled initial (Ⓐ, Ⓞ, Ⓩ) is a monogram: it tells you which letter the
// product starts with, which the label already does, and it is what made the
// list look like a set of boxes rather than a set of products.
//
//   ✳  Claude's eight-ray starburst (U+2733), in Anthropic clay.
//   ⬡  the hexagonal knot of OpenAI's mark (U+2B21), in OpenAI green.
//   ▣  OpenCode's square-in-square (U+25A3) — its mark is literally this.
//   Z  Z.ai's wordmark, which is the letter Z, in its near-black — or rather
//      in the terminal's own ink, because a near-black mark on a dark terminal
//      is invisible. See paints.
//
// A tool with no mark of its own gets no badge rather than an invented one, and
// its label still lines up: the badge column is always one cell.

type badge struct {
	glyph string
	hex   string // "rrggbb"; empty means the terminal's own foreground
}

// vendorBadges is keyed by TARGET id first and vendor key second. The two are
// not the same thing: OpenCode is an OpenAI-protocol client and the spec gives
// it icon "openai", so keying on the vendor alone would draw Codex's mark next
// to it. The id is what identifies the product; the vendor key is the fallback
// for a tool the spec has not taught us yet.
var vendorBadges = map[string]badge{
	// Products.
	"claude-cli": {glyph: "✳", hex: "D97757"},
	"desktop":    {glyph: "✳", hex: "D97757"},
	"codex":      {glyph: "⬡", hex: "10A37F"},
	"opencode":   {glyph: "▣", hex: "0F1115"},
	"zcode":      {glyph: "Z", hex: "0F1115"},
	// Vendor keys, for anything new the server names.
	"anthropic": {glyph: "✳", hex: "D97757"},
	"openai":    {glyph: "⬡", hex: "10A37F"},
}

// badgeFor picks the mark for a row: the target's own id, then the vendor the
// spec names, then nothing. Never a monogram — an invented mark is worse than a
// blank column, because it claims to be the product's logo.
func badgeFor(id, iconKey, label string) badge {
	for _, key := range []string{id, iconKey} {
		k := strings.ToLower(strings.TrimSpace(key))
		if k == "" {
			continue
		}
		if b, ok := vendorBadges[k]; ok {
			return b
		}
	}
	// One cell, so the label column stays aligned.
	_ = label
	return badge{glyph: " "}
}

// render paints the badge in the vendor's colour. Colour is skipped when the
// output is not a terminal, and NO_COLOR turns it off.
func (b badge) render() string { return b.renderOn(colorEnabled()) }

func (b badge) renderOn(color bool) string {
	if !color || !b.paints() {
		return b.glyph
	}
	r, g, bl, _ := hexRGB(b.hex)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", r, g, bl, b.glyph)
}

// paints reports whether the vendor's colour is worth applying.
//
// Some vendors' marks are black: Z.ai's is near-black, and so is OpenCode's.
// That is the right colour on paper, but a terminal is usually dark, and a
// black mark on a dark background is not a dim mark — it is no mark at all.
// The glyph was drawn and nobody could see it, which read as a missing icon.
//
// We cannot ask the terminal for its background, so the rule is the safe one:
// a colour this dark is not applied, and the mark is drawn in the terminal's
// own foreground. Nothing is lost — black-on-black was never carrying colour —
// and the shape, which is what identifies the product, survives on both a dark
// and a light theme.
const minBadgeLuminance = 0.05

func (b badge) paints() bool {
	r, g, bl, ok := hexRGB(b.hex)
	if !ok {
		return false
	}
	return luminance(r, g, bl) >= minBadgeLuminance
}

// luminance is the WCAG relative luminance of an sRGB colour, 0 (black) to 1
// (white). The sRGB curve is undone first, so the threshold is a real
// perceptual one and not an average of three channel bytes.
func luminance(r, g, bl int) float64 {
	linear := func(c int) float64 {
		v := float64(c) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(bl)
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
