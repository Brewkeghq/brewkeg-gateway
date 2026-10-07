package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/brewkeg/brewkeg-cli/internal/brewkeg"
)

func runSetup(args []string) int {
	var opts brewkeg.Options
	opts.APIKey = flagValue(args, "--api-key")
	opts.BaseURL = flagValue(args, "--base-url")
	opts.MainModel = flagValue(args, "--model")
	opts.FastModel = flagValue(args, "--fast-model")

	fmt.Println()
	fmt.Println("Brewkeg auto-configuration")
	fmt.Println()

	status := brewkeg.StatusAll()
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

	ids := make([]string, 0, len(idxs))
	for _, i := range idxs {
		ids = append(ids, status[i].ID)
	}

	b, results, err := brewkeg.Apply(ids, opts)
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

	fmt.Printf("\n  ✔ Backup saved: %s\n", brewkeg.BackupDir(b.ID))
	fmt.Println("  Undo anytime with:  brewkeg restore")

	fmt.Println()
	enabled := map[string]bool{}
	for _, r := range results {
		enabled[r.ID] = r.OK
	}
	if enabled["claude-cli"] {
		fmt.Println("  Restart your terminal, then verify:")
		fmt.Println(`    curl -s https://brewkeg.dev/v1/messages -H "Authorization: Bearer $ANTHROPIC_AUTH_TOKEN" \`)
		fmt.Println(`      -H "content-type: application/json" \`)
		fmt.Println(`      -d '{"model":"claude-opus-5","max_tokens":16,"messages":[{"role":"user","content":"PONG"}]}'`)
	}
	if enabled["codex"] {
		fmt.Println("  Verify codex:  codex exec --skip-git-repo-check \"Reply with: PONG\"")
	}
	fmt.Println()
	fmt.Println("  All set up. 🎉")
	fmt.Println()
	return 0
}

func looksLikeKey(k string) bool {
	l := strings.ToLower(k)
	return strings.HasPrefix(l, "bk_live") || strings.HasPrefix(l, "bk_test") || strings.HasPrefix(l, "sk-")
}
