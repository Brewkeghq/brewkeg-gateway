package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

var stdin = bufio.NewReader(os.Stdin)

func isTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// sttyCanonical puts the terminal into a state where a line can be read back:
// canonical input, a carriage return translated to a newline, echo on, and
// output post-processing on so a printed \n still returns the carriage.
//
// This is not paranoia. A terminal left in raw mode by an earlier run (or by
// a crash) sends CR for the Enter key, and a reader waiting for "\n" waits
// forever — the prompt looks frozen, with no error and nothing on screen to
// explain it. Every line prompt therefore asks for canonical mode first
// instead of assuming it.
//
// It is a var so a test can count the calls.
var sttyCanonical = func() {
	sttyRun("sane")
}

func promptLine(label, def string) string {
	sttyCanonical()
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, err := stdin.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return def
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// promptSecret reads without echoing. Falls back to a visible prompt when the
// terminal cannot be switched (pipes, some Windows terminals) — never silently
// prints the key.
func promptSecret(label string) string {
	sttyCanonical()
	if !isTTY() || runtime.GOOS == "windows" {
		fmt.Printf("%s: ", label)
		line, _ := stdin.ReadString('\n')
		return strings.TrimSpace(line)
	}
	fmt.Printf("%s: ", label)
	defer fmt.Println()

	stty := func(args ...string) *exec.Cmd {
		c := exec.Command("stty", args...)
		c.Stdin = os.Stdin
		_ = c.Run()
		return c
	}
	stty("-echo")
	defer stty("echo")

	line, _ := stdin.ReadString('\n')
	return strings.TrimSpace(line)
}

func confirm(label string, def bool) bool {
	sttyCanonical()
	s := "y/N"
	if def {
		s = "Y/n"
	}
	fmt.Printf("%s [%s]: ", label, s)
	line, err := stdin.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return def
	}
	line = strings.ToLower(strings.TrimSpace(line))
	switch line {
	case "":
		return def
	case "y", "yes":
		return true
	default:
		return false
	}
}

type choice struct {
	label   string
	checked bool
	hint    string
	// icon is the vendor key the server sends with the target ("anthropic",
	// "openai"). The terminal draws a badge from it; the desktop app draws
	// the vendor's own artwork.
	icon string
}

// multiSelect gives the arrow-key checklist on a unix TTY, and a plain
// "1,2,3" prompt everywhere else (pipes, CI, Windows).
func multiSelect(title string, items []choice) []int {
	if !isTTY() || runtime.GOOS == "windows" {
		return multiSelectPlain(title, items)
	}
	return multiSelectRaw(title, items)
}

func multiSelectPlain(title string, items []choice) []int {
	fmt.Println(title)
	for i, c := range items {
		fmt.Printf("  %d) %s %s\n", i+1, badgeFor(c.icon, c.label).render(), c.label)
	}
	fmt.Print("Select (comma-separated numbers, or 'all'): ")
	line, _ := stdin.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	var out []int
	if line == "all" || line == "a" || line == "" {
		for i := range items {
			out = append(out, i)
		}
		return out
	}
	for _, part := range strings.Split(line, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && n >= 1 && n <= len(items) {
			out = append(out, n-1)
		}
	}
	return out
}

// sttySaved holds the terminal settings captured before the prompt switched
// the tty to raw mode, so they can be put back exactly afterwards.
var sttySaved string

// sttyRawActive says whether the tty is currently in raw mode, so a restore
// is never applied twice.
var sttyRawActive bool

// sttyRun runs stty against the controlling terminal.
func sttyRun(args ...string) {
	c := exec.Command("stty", args...)
	c.Stdin = os.Stdin
	_ = c.Run()
}

// sttyRawArgs are the stty arguments that put the terminal in raw mode for
// key-at-a-time reading.
//
// `opost` is not optional. `raw` clears it, and opost is what turns a printed
// \n into \r\n on the way out. With it off the cursor drops a row but keeps
// its column, so every line the prompt draws starts where the previous one
// ended and the checklist renders as a staircase. Raw is for the input side,
// not the output side.
func sttyRawArgs() []string { return []string{"raw", "-echo", "opost"} }

// sttyRestoreArgs are the stty arguments that put the terminal back. The
// settings captured before raw mode are restored exactly; `sane` is the
// fallback for when they could not be read.
func sttyRestoreArgs(saved string) []string {
	if saved != "" {
		return []string{saved}
	}
	return []string{"sane"}
}

// sttyRaw switches the terminal to raw mode for key-at-a-time reading, and
// restores the previous settings when it is switched off.
//
// The old code restored with `stty -echo`, which only flips echo and left the
// terminal in raw mode afterwards (no isig, no icanon, no opost) — a shell
// with no echo and no ctrl-c. The exact prior settings are saved and restored
// instead.
func sttyRaw(on bool) {
	if on {
		if sttyRawActive {
			return
		}
		c := exec.Command("stty", "-g")
		c.Stdin = os.Stdin
		if out, err := c.Output(); err == nil {
			sttySaved = strings.TrimSpace(string(out))
		}
		sttyRun(sttyRawArgs()...)
		sttyRawActive = true
		return
	}
	if !sttyRawActive {
		return
	}
	sttyRawActive = false
	sttyRun(sttyRestoreArgs(sttySaved)...)
	sttySaved = ""
}

// decodeKey pulls one key off the front of pending.
//
// A key is either a single byte (space, enter, ctrl-c) or a three-byte escape
// sequence (an arrow). When only part of a sequence has arrived, the bytes
// are left in pending and the caller reads more: a read that returned a lone
// ESC used to leave the code with nothing to match, and the arrow key was
// dropped on the floor — the user pressed down and the cursor stayed put.
func decodeKey(pending []byte) (action string, consumed int, ok bool) {
	if len(pending) == 0 {
		return "", 0, false
	}
	if pending[0] == 0x1b {
		if len(pending) < 3 {
			return "", 0, false // wait for the rest of the sequence
		}
		switch pending[2] {
		case 'A':
			return "up", 3, true
		case 'B':
			return "down", 3, true
		}
		return "", 3, true // some other escape sequence: drop it
	}
	switch pending[0] {
	case ' ':
		return "toggle", 1, true
	case '\r', '\n':
		return "confirm", 1, true
	case 3:
		return "cancel", 1, true
	}
	return "", 1, true // a key the checklist does not use: drop it
}

func multiSelectRaw(title string, items []choice) []int {
	cursor := 0
	for i := range items {
		if !items[i].checked {
			cursor = i
			break
		}
	}
	sttyRaw(true)
	defer func() { sttyRaw(false); fmt.Println() }()

	buf := make([]byte, 16)
	// pending holds bytes that have arrived but not yet formed a whole key.
	// An arrow key is three bytes, and the terminal may hand them over in
	// pieces: a read that returned only ESC left the code with nothing to
	// match, and the keypress was dropped on the floor. The user pressed
	// down and the cursor did not move.
	var pending []byte
	for {
		fmt.Print("\x1b[2J\x1b[H") // clear + home, keeps the list from smearing
		fmt.Println(title)
		fmt.Println("  ↑/↓ move · space toggles ✓ · enter confirm")
		for i, c := range items {
			mark := "☐"
			if c.checked {
				mark = "✓"
			}
			cur := " "
			if i == cursor {
				cur = "›"
			}
			suffix := ""
			if c.hint != "" {
				suffix = "  (" + c.hint + ")"
			}
			fmt.Printf(" %s %s %s %s%s\n", cur, mark, badgeFor(c.icon, c.label).render(), c.label, suffix)
		}
		fmt.Print(" ")

		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return checkedIndices(items)
		}
		pending = append(pending, buf[:n]...)
		for len(pending) > 0 {
			action, consumed, ok := decodeKey(pending)
			if !ok {
				break // an incomplete escape sequence: wait for the rest
			}
			pending = pending[consumed:]
			switch action {
			case "up":
				cursor = (cursor - 1 + len(items)) % len(items)
			case "down":
				cursor = (cursor + 1) % len(items)
			case "toggle":
				items[cursor].checked = !items[cursor].checked
			case "confirm":
				return checkedIndices(items)
			case "cancel":
				// Put the terminal back before leaving: os.Exit skips the
				// deferred restore, and a shell left in raw mode has no echo
				// and no ctrl-c. An empty selection would read as "disconnect
				// everything", so this must not fall through to the caller.
				sttyRaw(false)
				fmt.Println()
				os.Exit(1)
			}
		}
	}
}

// checkedIndices is the list of the rows the user ticked.
func checkedIndices(items []choice) []int {
	var out []int
	for i, c := range items {
		if c.checked {
			out = append(out, i)
		}
	}
	return out
}
