package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/brewkeg/brewkeg-cli/internal/brewkeg"
)

func runSetup(args []string) int {
	spec, _ := brewkeg.FetchSpec(context.Background(), brewkeg.BaseURL())
	status := brewkeg.StatusAllFrom(spec)

	var opts brewkeg.Options
	opts.APIKey = flagValue(args, "--api-key")
	opts.BaseURL = flagValue(args, "--base-url")
	opts.MainModel = flagValue(args, "--model")
	opts.FastModel = flagValue(args, "--fast-model")

	fmt.Println()
	fmt.Println("Brewkeg auto-configuration")
	fmt.Println()

	var items []choice
	for _, s := range status {
		hint := "not detected — will write config anyway"
		if s.Installed {
			hint = "found"
		}
		items = append(items, choice{label: s.Label, checked: s.Installed, hint: hint})
	}

	idxs := multiSelect("Which services do you want to configure?", items)
	if len(idxs) == 0 {
		fmt.Println("\nNothing selected. Nothing changed.")
		return 1
	}

	if opts.APIKey == "" {
		opts.APIKey = promptSecret("Enter your brewkeg API key (from brewkeg.dev/dashboard)")
	}
	if opts.APIKey == "" {
		fmt.Println("No API key given. Nothing changed.")
		return 1
	}
	if !looksLikeKey(opts.APIKey) && !confirm("That doesn't look like a brewkeg key. Continue anyway?", false) {
		fmt.Println("Cancelled. Nothing changed.")
		return 1
	}
	if opts.BaseURL == "" {
		opts.BaseURL = strings.TrimRight(promptLine("Gateway base URL", brewkeg.BaseURL()), "/")
	}

	// Test the key against the live gateway before writing anything. A typo
	// baked into ~/.codex/config.toml breaks the user's editor silently.
	if !hasFlag(args, "--skip-check") {
		fmt.Println("\n  Testing your key…")
		res := brewkeg.CheckKey(context.Background(), brewkeg.BaseURL(), opts.APIKey)
		fmt.Printf("  %s\n", res.Message)
		if !res.OK && !res.Valid {
			fmt.Println("\n  Nothing was changed. Check the key and try again.")
			return 1
		}
		if !res.OK && res.Valid && !confirm("  Connect anyway?", false) {
			fmt.Println("\n  Nothing was changed.")
			return 1
		}
		fmt.Println()
	}

	ids := make([]string, 0, len(idxs))
	for _, i := range idxs {
		ids = append(ids, status[i].ID)
	}

	b, results, err := brewkeg.ApplyWithSpec(spec, ids, opts)
	fmt.Println()
	for _, r := range results {
		switch {
		case !r.OK:
			fmt.Fprintf(os.Stderr, "  ✖ %s: %s\n", r.Label, r.Error)
		case r.Manual != "":
			fmt.Printf("  ✔ %s is configured by hand in the app:\n", r.Label)
			for _, line := range strings.Split(r.Manual, "\n") {
				fmt.Println("    " + line)
			}
		default:
			fmt.Printf("  ✔ %s → %s\n", r.Label, strings.Join(r.Paths, " + "))
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✖ could not save backup: %v\n", err)
		return 1
	}

	if hints := brewkeg.RestartHintsFor(ids, pathsOf(results)); len(hints) > 0 {
		fmt.Println("\n  Restart these to pick up the change:")
		for _, h := range hints {
			fmt.Printf("    • %s — %s\n", h.What, h.Action)
		}
	}

	fmt.Printf("\n  ✔ Backup saved: %s\n", brewkeg.BackupDir(b.ID))
	fmt.Println("  Undo anytime with:  brewkeg restore")

	fmt.Println()
	fmt.Println("  All set up. 🎉")
	fmt.Println()
	return 0
}

// pathsOf collects the files a run actually touched, so the restart hints know
// whether the user's shell was involved.
func pathsOf(results []brewkeg.ApplyResult) []string {
	var out []string
	for _, r := range results {
		if r.OK {
			out = append(out, r.Paths...)
		}
	}
	return out
}

func looksLikeKey(k string) bool {
	l := strings.ToLower(k)
	return strings.HasPrefix(l, "bk_live") || strings.HasPrefix(l, "bk_test") || strings.HasPrefix(l, "sk-")
}
