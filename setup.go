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
	opts.SonnetModel = flagValue(args, "--sonnet-model")
	opts.FastModel = flagValue(args, "--fast-model")

	// ~/.brewkeg/config.json is the base; flags and prompts override it. This
	// is what makes `brewkeg setup --yes` work with no input at all.
	cfg := brewkeg.LoadUserConfig()
	opts = cfg.Resolve(opts)

	// --yes means "do not ask": take the key and the target list from the
	// config file, and refuse to invent either.
	yes := hasFlag(args, "--yes") || hasFlag(args, "-y")

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

	var picked []string
	if yes {
		picked = brewkeg.ConfiguredTargets(spec)
		if len(picked) == 0 {
			fmt.Printf("No targets in %s. Nothing changed.\n", brewkeg.UserConfigPath())
			return 1
		}
		for _, id := range picked {
			fmt.Println("  ·", brewkeg.LabelFor(spec, id))
		}
	} else {
		idxs := multiSelect("Which services do you want to configure?", items)
		if len(idxs) == 0 {
			// Selecting nothing is a real request when something is already
			// connected — it means "turn brewkeg off everywhere". Only a no-op
			// when there was nothing on to begin with.
			if !brewkeg.AnyEnabled() {
				fmt.Println("\nNothing selected. Nothing changed.")
				return 1
			}
			fmt.Println("\nNo services selected — disconnecting everything brewkeg currently points at.")
		}
		for _, i := range idxs {
			picked = append(picked, status[i].ID)
		}
	}

	if opts.APIKey == "" && yes {
		fmt.Printf("No apiKey in %s. Nothing changed.\n", brewkeg.UserConfigPath())
		return 1
	}
	// Disconnecting needs no credential: once every tool is pointed away from
	// us there is nothing left to authenticate. Demanding a key to undo our own
	// changes would strand anyone whose key has just expired.
	disconnectOnly := len(picked) == 0
	if opts.APIKey == "" && !disconnectOnly {
		opts.APIKey = promptSecret("Enter your brewkeg API key (from brewkeg.dev/dashboard)")
	}
	if opts.APIKey == "" && !disconnectOnly {
		fmt.Println("No API key given. Nothing changed.")
		return 1
	}
	if !disconnectOnly && !yes && !looksLikeKey(opts.APIKey) && !confirm("That doesn't look like a brewkeg key. Continue anyway?", false) {
		fmt.Println("Cancelled. Nothing changed.")
		return 1
	}
	if opts.BaseURL == "" && !yes {
		opts.BaseURL = strings.TrimRight(promptLine("Gateway base URL", brewkeg.BaseURL()), "/")
	}

	// Test the key against the live gateway before writing anything. A typo
	// baked into ~/.codex/config.toml breaks the user's editor silently.
	//
	// Only a definitive rejection stops the run. "Could not reach brewkeg" is
	// not a statement about the key, so treating it as one would strand an
	// offline user — and the old code did exactly that.
	if !disconnectOnly && !hasFlag(args, "--skip-check") {
		fmt.Println("\n  Testing your key…")
		res := brewkeg.CheckKey(context.Background(), brewkeg.BaseURL(), opts.APIKey)
		fmt.Printf("  %s\n", res.Message)
		switch {
		case res.Blocked():
			fmt.Println("\n  Nothing was changed. Check the key and try again.")
			return 1
		case !res.Reachable:
			fmt.Println("  Could not reach brewkeg, so the key is untested.")
			if yes || confirm("  Save it anyway?", false) {
				fmt.Println("  Saving anyway (--yes).")
			} else {
				fmt.Println("\n  Nothing was changed.")
				return 1
			}
		case res.Valid && !res.OK:
			if yes || confirm("  Connect anyway?", false) {
				fmt.Println("  Connecting anyway (--yes).")
			} else {
				fmt.Println("\n  Nothing was changed.")
				return 1
			}
		}
		fmt.Println()
	}

	b, results, err := brewkeg.ApplyWithSpec(spec, picked, opts)
	fmt.Println()
	for _, r := range results {
		switch {
		case !r.OK:
			fmt.Fprintf(os.Stderr, "  ✖ %s: %s\n", r.Label, r.Error)
		case r.Removed:
			fmt.Printf("  ✔ %s disconnected — brewkeg removed from %s\n", r.Label, strings.Join(r.Paths, " + "))
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

	if hints := brewkeg.RestartHintsFor(picked, pathsOf(results)); len(hints) > 0 {
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
