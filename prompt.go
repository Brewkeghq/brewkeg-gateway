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

func promptLine(label, def string) string {
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
		fmt.Printf("  %d) %s\n", i+1, c.label)
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

func multiSelectRaw(title string, items []choice) []int {
	cursor := 0
	for i := range items {
		if !items[i].checked {
			cursor = i
			break
		}
	}
	sttyRaw := func(on bool) {
		args := []string{"-echo"}
		if on {
			args = []string{"raw", "-echo"}
		}
		c := exec.Command("stty", args...)
		c.Stdin = os.Stdin
		_ = c.Run()
	}
	sttyRaw(true)
	defer func() { sttyRaw(false); fmt.Println() }()

	buf := make([]byte, 3)
	for {
		fmt.Print("\x1b[2J\x1b[H") // clear + home, keeps the list from smearing
		fmt.Println(title)
		fmt.Println("  ↑/↓ move · space toggle · enter confirm")
		for i, c := range items {
			box := "⬜"
			if c.checked {
				box = "🟩"
			}
			cur := " "
			if i == cursor {
				cur = "›"
			}
			suffix := ""
			if c.hint != "" {
				suffix = "  (" + c.hint + ")"
			}
			fmt.Printf(" %s %s %s%s\n", cur, box, c.label, suffix)
		}
		fmt.Print(" ")

		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			var out []int
			for i, c := range items {
				if c.checked {
					out = append(out, i)
				}
			}
			return out
		}
		switch {
		case n >= 3 && buf[0] == 0x1b && buf[1] == '[':
			switch buf[2] {
			case 'A':
				cursor = (cursor - 1 + len(items)) % len(items)
			case 'B':
				cursor = (cursor + 1) % len(items)
			}
		case buf[0] == ' ':
			items[cursor].checked = !items[cursor].checked
		case buf[0] == '\r' || buf[0] == '\n':
			var out []int
			for i, c := range items {
				if c.checked {
					out = append(out, i)
				}
			}
			return out
		case buf[0] == 3: // ctrl-c
			os.Exit(1)
		}
	}
}
