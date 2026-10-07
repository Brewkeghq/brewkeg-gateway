package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/brewkeg/brewkeg-cli/internal/brewkeg"
)

// headless is `gateway --apply`: no window, no prompts, everything from
// ~/.brewkeg/config.json.
//
// It exists because the window is a fine way to configure one machine and a
// terrible way to configure fifty. Same engine, same key gate, same backup —
// so a script and a click cannot produce different config files.
func headless() int {
	cfg := brewkeg.LoadUserConfig()
	if cfg.APIKey == "" {
		// The key store is the fallback, so a machine already configured
		// through the window can be re-run from a script with no config file.
		cfg.APIKey = brewkeg.StoredKey()
	}
	if cfg.APIKey == "" {
		fmt.Fprintf(os.Stderr, "No apiKey in %s and none stored. Nothing changed.\n", brewkeg.UserConfigPath())
		return 1
	}

	base := brewkeg.BaseURL()
	if cfg.BaseURL != "" {
		base = cfg.BaseURL
	}
	spec, _ := brewkeg.FetchSpec(context.Background(), base)
	ids := brewkeg.ConfiguredTargets(spec)
	if len(ids) == 0 {
		fmt.Fprintf(os.Stderr, "No known targets in %s. Nothing changed.\n", brewkeg.UserConfigPath())
		return 1
	}

	opts := cfg.Resolve(brewkeg.Options{APIKey: cfg.APIKey, BaseURL: base})

	// The same gate the window uses: only a definitive rejection stops a write.
	check := brewkeg.CheckKey(context.Background(), base, opts.APIKey)
	fmt.Println(" ", check.Message)
	if check.Blocked() {
		fmt.Fprintln(os.Stderr, "  Nothing was changed.")
		return 1
	}
	if !check.Reachable {
		fmt.Fprintln(os.Stderr, "  Warning: brewkeg did not answer, so the key is untested. Saving anyway.")
	}

	fmt.Println("  Targets:", strings.Join(labelled(spec, ids), ", "))
	b, results, err := brewkeg.ApplyWithSpec(spec, ids, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✖ could not save backup: %v\n", err)
		return 1
	}

	failed := 0
	for _, r := range results {
		if !r.OK {
			failed++
			fmt.Fprintf(os.Stderr, "  ✖ %s: %s\n", r.Label, r.Error)
			continue
		}
		if r.Manual != "" {
			fmt.Printf("  ✔ %s — configure it by hand:\n", r.Label)
			for _, line := range strings.Split(r.Manual, "\n") {
				fmt.Println("    " + line)
			}
			continue
		}
		fmt.Printf("  ✔ %s → %s\n", r.Label, strings.Join(r.Paths, " + "))
	}
	if failed == len(results) {
		fmt.Fprintln(os.Stderr, "  Nothing could be configured. Your files are untouched.")
		return 1
	}

	if hints := brewkeg.RestartHintsFor(ids, pathsOfResults(results)); len(hints) > 0 {
		fmt.Println("\n  Restart these to pick up the change:")
		for _, h := range hints {
			fmt.Printf("    • %s — %s\n", h.What, h.Action)
		}
	}
	fmt.Println("\n  Backup saved:", brewkeg.BackupDir(b.ID))
	fmt.Println("  Undo with:    gateway --undo")
	return 0
}

// --undo restores the latest backup without opening a window. People automate
// the apply; they will want to automate the rollback too.
func headlessUndo() int {
	b, err := brewkeg.LatestBackup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Nothing to undo: %v\n", err)
		return 1
	}
	fmt.Println("Restoring", b.ID)
	results, err := b.Restore(false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✖ %v\n", err)
		return 1
	}
	for _, r := range results {
		switch r.Action {
		case "removed":
			fmt.Printf("  ✔ %s — removed\n", r.Path)
		case "missing":
			fmt.Printf("  · %s — was not there, nothing to do\n", r.Path)
		default:
			fmt.Printf("  ✔ %s — restored\n", r.Path)
		}
	}
	fmt.Println("  Your configs are back to how they were.")
	return 0
}

func labelled(s brewkeg.Spec, ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, brewkeg.LabelFor(s, id))
	}
	return out
}

func pathsOfResults(rs []brewkeg.ApplyResult) []string {
	var out []string
	for _, r := range rs {
		if r.OK {
			out = append(out, r.Paths...)
		}
	}
	return out
}
