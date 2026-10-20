package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/brewkeghq/brewkeg-gateway/internal/brewkeg"
)

// menuItem is one row of the interactive menu.
type menuItem struct {
	label string
	run   func() int
}

// runMenu is what `brewkeg` with no arguments does.
func runMenu() int {
	agents := brewkeg.Agents()

	items := make([]menuItem, 0, len(agents)+8)
	for _, a := range agents {
		agent := a
		items = append(items, menuItem{
			label: agent.Label,
			run:   func() int { return runAgent(agent.ID, nil) },
		})
	}
	items = append(items,
		menuItem{label: "Models", run: func() int { return runModels(nil) }},
		menuItem{label: "Usage", run: func() int { return runUsage(nil) }},
		menuItem{label: "Doctor", run: func() int { return runDoctor(nil) }},
		menuItem{label: "Desktop status", run: func() int { return runDesktop([]string{"status"}) }},
		menuItem{label: "Connect every tool", run: func() int { return runSetup(nil) }},
		menuItem{label: "Quit", run: func() int { return 0 }},
	)

	for {
		fmt.Println()
		fmt.Println("brewkeg — run coding agents on brewkeg")
		fmt.Println()
		for i, it := range items {
			fmt.Printf("  %2d) %s\n", i+1, it.label)
		}
		fmt.Print("\nChoose (number, or q to quit): ")

		line, _ := stdin.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "q") {
			return 0
		}
		n, err := strconv.Atoi(line)
		if err != nil || n < 1 || n > len(items) {
			fmt.Println("Not a number on the list.")
			continue
		}
		if code := items[n-1].run(); code != 0 {
			return code
		}
	}
}

// chooseOne asks for a single pick from a numbered list. Returns -1 when the
// user backs out.
func chooseOne(title string, options []string) int {
	fmt.Println()
	fmt.Println(title)
	for i, o := range options {
		fmt.Printf("  %d) %s\n", i+1, o)
	}
	fmt.Print("Choose (number, or q to quit): ")
	line, _ := stdin.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" || strings.EqualFold(line, "q") {
		return -1
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(options) {
		return -1
	}
	return n - 1
}

var _ = os.Stdout
