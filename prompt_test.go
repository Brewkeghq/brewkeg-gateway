package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brewkeghq/brewkeg-gateway/internal/brewkeg"
)

// The checklist is drawn with plain \n, so the terminal has to turn those into
// \r\n on the way out. That translation is opost, and `stty raw` clears it —
// so raw mode has to ask for opost back, or every line the prompt prints
// starts at the column the previous line ended on and the list renders as a
// staircase. This test is the guard: delete "opost" from sttyRawArgs and it
// goes red.
func TestSttyRawKeepsOutputPostProcessing(t *testing.T) {
	args := sttyRawArgs()
	joined := strings.Join(args, " ")

	if !contains(args, "raw") {
		t.Fatalf("raw mode is not requested, stty args = %q", joined)
	}
	if !contains(args, "-echo") {
		t.Fatalf("echo is not off, the typed key would be printed, stty args = %q", joined)
	}
	if !contains(args, "opost") {
		t.Fatalf("opost is missing: the terminal stops turning \\n into \\r\\n and the checklist renders as a staircase, stty args = %q", joined)
	}
}

// The prompt must give the terminal back exactly as it found it. The old code
// restored with `stty -echo`, which only flips echo and leaves the tty in raw
// mode (no isig, no icanon, no opost) — a shell with no echo and no ctrl-c.
func TestSttyRestorePutsTheTerminalBack(t *testing.T) {
	saved := "gfmt1:cflag=...:"

	got := sttyRestoreArgs(saved)
	if len(got) != 1 || got[0] != saved {
		t.Fatalf("the captured settings were not restored, got %v", got)
	}

	// Nothing captured (stty -g failed): sane is the fallback, not a guess
	// that leaves the tty half-configured.
	fallback := sttyRestoreArgs("")
	if len(fallback) != 1 || fallback[0] != "sane" {
		t.Fatalf("expected the sane fallback, got %v", fallback)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// A terminal left in raw mode by an earlier run sends CR for the Enter key,
// and a reader waiting for "\n" waits forever. The prompt then looks frozen:
// no error, nothing on screen, the user typing into nothing.
//
// Every line prompt therefore has to ask for canonical mode before it reads.
// This test is the guard: remove the sttyCanonical() call from any of the
// three readers and it goes red.
func TestLinePromptsAskForCanonicalMode(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("hello\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}

	oldStdin, oldReader := os.Stdin, stdin
	defer func() { os.Stdin, stdin = oldStdin, oldReader }()
	os.Stdin = f

	// Each prompt gets its own line: the reader is drained by the first one.
	rewind := func() {
		if _, err := f.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		stdin = bufio.NewReader(f)
	}

	calls := 0
	old := sttyCanonical
	sttyCanonical = func() { calls++ }
	defer func() { sttyCanonical = old }()

	rewind()
	if got := promptLine("Gateway base URL", "https://brewkeg.dev"); got != "hello" {
		t.Fatalf("promptLine read %q", got)
	}
	rewind()
	if got := promptSecret("API key"); got != "hello" {
		t.Fatalf("promptSecret read %q", got)
	}
	rewind()
	confirm("Continue anyway?", false)

	if calls != 3 {
		t.Fatalf("the line prompts asked for canonical mode %d times, want 3: a terminal left in raw mode never sends \\n, so the read never returns", calls)
	}
}

// The row carries the vendor's mark, not a monogram. The id identifies the
// product and wins over the vendor key the server sends: OpenCode speaks the
// OpenAI protocol and carries icon "openai", so keying on the vendor alone
// would draw Codex's mark next to it.
func TestBadgeForDrawsTheVendorsMark(t *testing.T) {
	if got := badgeFor("claude-cli", "anthropic", "Claude Code").glyph; got != "✳" {
		t.Fatalf("claude-cli badge is %q, want ✳", got)
	}
	if got := badgeFor("codex", "openai", "Codex CLI").glyph; got != "⬡" {
		t.Fatalf("codex badge is %q, want ⬡", got)
	}
	if got := badgeFor("opencode", "openai", "OpenCode").glyph; got != "▣" {
		t.Fatalf("opencode badge is %q, want ▣ — the id decides the mark, not the shared vendor key", got)
	}
	if got := badgeFor("zcode", "", "ZCode").glyph; got != "Z" {
		t.Fatalf("zcode badge is %q, want Z", got)
	}
	// A tool the spec has not taught us yet still gets its vendor's mark.
	if got := badgeFor("something-new", "anthropic", "New Tool").glyph; got != "✳" {
		t.Fatalf("an unknown id with vendor anthropic got %q, want ✳", got)
	}
}

// No mark of its own means no badge. An invented monogram claims to be the
// product's logo, which is worse than a blank column — but the column is still
// one cell wide, so the labels below it stay aligned.
func TestBadgeForInventsNothing(t *testing.T) {
	for _, label := range []string{"ZCode", "Claude Code", ""} {
		b := badgeFor("", "", label)
		if b.glyph != " " {
			t.Fatalf("an unnamed target with label %q got badge %q, want a single blank cell", label, b.glyph)
		}
		if b.hex != "" {
			t.Fatalf("a blank badge carries colour %q", b.hex)
		}
	}
}

// The badge colour is the vendor's, and it is only painted when the output
// is a terminal — a piped checklist must stay plain text.
func TestBadgeColourIsTheVendorColour(t *testing.T) {
	b := badgeFor("claude-cli", "anthropic", "Claude Code")
	if b.hex != "D97757" {
		t.Fatalf("claude-cli colour is %q, want D97757", b.hex)
	}
	r, g, bl, ok := hexRGB(b.hex)
	if !ok || r != 0xD9 || g != 0x77 || bl != 0x57 {
		t.Fatalf("D97757 parsed as %d,%d,%d (ok=%v)", r, g, bl, ok)
	}
	if got := badgeFor("codex", "openai", "Codex CLI").hex; got != "10A37F" {
		t.Fatalf("openai colour is %q, want 10A37F", got)
	}
	if got := badgeFor("opencode", "openai", "OpenCode").hex; got != "0F1115" {
		t.Fatalf("opencode colour is %q, want 0F1115", got)
	}
}

// A vendor whose colour is black would paint its mark black. On a dark
// terminal that is not a dim badge, it is no badge: the glyph is drawn and
// nobody sees it, which is what "the icons don't show" looked like. Such a
// colour is dropped and the mark falls back to the terminal's own foreground,
// so the shape still shows on a dark theme and on a light one.
func TestBadgeKeepsADarkMarkVisible(t *testing.T) {
	for _, id := range []string{"opencode", "zcode"} {
		b := badgeFor(id, "", id)
		if b.glyph == " " {
			t.Fatalf("%s has no mark at all", id)
		}
		if b.paints() {
			t.Fatalf("%s is painted %q: black on a dark terminal is invisible", id, b.hex)
		}
		if got := b.renderOn(true); got != b.glyph {
			t.Fatalf("%s rendered %q with colour on, want the bare glyph %q", id, got, b.glyph)
		}
	}

	// The colours that are actually a colour still get painted.
	for _, id := range []string{"claude-cli", "codex"} {
		if b := badgeFor(id, "", id); !b.paints() {
			t.Fatalf("%s colour %q was dropped, want it painted", id, b.hex)
		}
	}

	// Only the dark end is guarded: dark terminals are the common case, so a
	// pale mark is left alone rather than guessed at.
	white := badge{glyph: "Z", hex: "FFFFFF"}
	if !white.paints() {
		t.Fatal("white was dropped; only near-background colours should be")
	}
	nearBlack := badge{glyph: "Z", hex: "050505"}
	if nearBlack.paints() {
		t.Fatal("near-black was painted")
	}
	colourless := badge{glyph: "Z"}
	if colourless.paints() {
		t.Fatal("a badge with no colour claims to be painted")
	}
}

// The tick on a row means "brewkeg will manage this", and it starts on what
// brewkeg already has. Pre-ticking every tool that merely exists on the
// machine made the list read as a list of things already done: the user
// pressed space on the one tool they wanted, which turned it off, and the
// tools they never touched were the ones that got configured.
func TestChecklistTicksOnlyWhatIsAlreadyConnected(t *testing.T) {
	items := checklistItems([]brewkeg.TargetStatus{
		{ID: "claude-cli", Label: "Claude Code", Installed: true},
		{ID: "codex", Label: "Codex CLI", Installed: true, Enabled: true},
		{ID: "zcode", Label: "ZCode"},
	})

	if len(items) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(items))
	}
	if items[0].checked {
		t.Fatal("an installed but unconnected tool is pre-ticked: pressing space on the tool the user wants would turn it off")
	}
	if !items[1].checked {
		t.Fatal("a tool brewkeg already has is not pre-ticked: a re-run would disconnect it")
	}
	if items[2].checked {
		t.Fatal("a tool that is not installed is pre-ticked")
	}
}

// A read can hand over a lone ESC, and the arrow key is three bytes. The
// bytes that do not form a whole key have to wait for the next read, or the
// keypress is dropped and the cursor does not move under the user.
func TestDecodeKeyWaitsForASplitEscapeSequence(t *testing.T) {
	if _, _, ok := decodeKey([]byte{0x1b}); ok {
		t.Fatal("a lone ESC was decoded as a key; the rest of the sequence would be lost")
	}
	if _, _, ok := decodeKey([]byte{0x1b, '['}); ok {
		t.Fatal("half an arrow key was decoded; the cursor would skip a row")
	}

	action, consumed, ok := decodeKey([]byte{0x1b, '[', 'B'})
	if !ok || action != "down" || consumed != 3 {
		t.Fatalf("down arrow decoded as action=%q consumed=%d ok=%v", action, consumed, ok)
	}
	action, _, ok = decodeKey([]byte{0x1b, '[', 'A'})
	if !ok || action != "up" {
		t.Fatalf("up arrow decoded as action=%q ok=%v", action, ok)
	}
}

// The rest of a keypress after an arrow is not swallowed: space must still
// toggle the row the cursor is on.
func TestDecodeKeyKeepsWhatFollowsAnArrow(t *testing.T) {
	action, consumed, ok := decodeKey([]byte{0x1b, '[', 'B', ' '})
	if !ok || action != "down" || consumed != 3 {
		t.Fatalf("decoded action=%q consumed=%d ok=%v, want down/3", action, consumed, ok)
	}
	rest := []byte{0x1b, '[', 'B', ' '}[consumed:]
	action, _, ok = decodeKey(rest)
	if !ok || action != "toggle" {
		t.Fatalf("the space after the arrow decoded as action=%q ok=%v, want toggle", action, ok)
	}
}

func TestDecodeKeySingleByteKeys(t *testing.T) {
	for _, tc := range []struct {
		b    byte
		want string
	}{
		{' ', "toggle"},
		{'\r', "confirm"},
		{'\n', "confirm"},
		{3, "cancel"},
	} {
		action, consumed, ok := decodeKey([]byte{tc.b})
		if !ok || action != tc.want || consumed != 1 {
			t.Fatalf("byte %d decoded as action=%q consumed=%d ok=%v, want %q", tc.b, action, consumed, ok, tc.want)
		}
	}
}
