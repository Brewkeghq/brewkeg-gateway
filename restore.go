package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/brewkeghq/brewkeg-gateway/internal/brewkeg"
)

func runRestore(args []string) int {
	dry := hasFlag(args, "--dry-run")
	id := flagValue(args, "--backup")

	var b *brewkeg.Backup
	var err error
	if id != "" {
		b, err = brewkeg.LoadBackup(id)
	} else {
		b, err = brewkeg.LatestBackup()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "✖ %v\n", err)
		return 1
	}

	fmt.Printf("Backup %s  (%s, targets: %s)\n", b.ID, b.CreatedAt, strings.Join(b.Targets, ", "))
	for _, e := range b.Entries {
		state := "created by brewkeg"
		if e.Existed {
			state = "will be overwritten"
		}
		fmt.Printf("  %s  [%s]\n", e.Path, state)
	}

	if dry {
		fmt.Println("\n(dry run — nothing written)")
		return 0
	}
	if !confirm(fmt.Sprintf("\nRestore these files to how they were on %s?", b.CreatedAt), false) {
		fmt.Println("Cancelled. Nothing changed.")
		return 1
	}

	results, err := b.Restore(false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✖ restore failed part-way: %v\n", err)
	}
	fmt.Println()
	for _, r := range results {
		fmt.Printf("  ✔ %s — %s\n", r.Path, r.Action)
	}

	// Drop the backup now that it is spent, so a later restore can't resurrect it.
	_ = os.RemoveAll(brewkeg.BackupDir(b.ID))

	fmt.Println("\n  Restored. Your configs are back to their pre-brewkeg state.")
	fmt.Println("  Restart your terminal if you had shell exports added.")
	fmt.Println()
	return 0
}

func runBackups() int {
	all, err := brewkeg.ListBackups()
	if err != nil {
		fmt.Fprintf(os.Stderr, "✖ %v\n", err)
		return 1
	}
	if len(all) == 0 {
		fmt.Println("No backups yet. Run:  brewkeg setup")
		return 0
	}
	fmt.Printf("%-24s %-22s %s\n", "ID", "WHEN", "TARGETS")
	for _, b := range all {
		fmt.Printf("%-24s %-22s %s\n", b.ID, b.CreatedAt, strings.Join(b.Targets, ", "))
	}
	fmt.Println("\nRestore one with:  brewkeg restore --backup <id>")
	return 0
}

func runStatus() int {
	fmt.Print("Brewkeg configuration\n\n")
	spec, _ := brewkeg.FetchSpec(context.Background(), brewkeg.BaseURL())
	for _, s := range brewkeg.StatusAllFrom(spec) {
		mark := "○"
		state := "not detected on this machine"
		if s.Installed {
			mark = "●"
			if s.Enabled {
				state = "configured"
			} else {
				state = "installed, not configured"
			}
			if s.Note != "" {
				state = s.Note
			}
		}
		fmt.Printf("  %s %-18s %s  (%s)\n", mark, s.Label, state, s.Path)
	}

	all, _ := brewkeg.ListBackups()
	fmt.Printf("\n  backups: %d  (%s)\n", len(all), brewkeg.BackupsRoot())
	fmt.Println("  gateway: " + brewkeg.BaseURL())
	return 0
}

func runUninstall(args []string) int {
	b, err := brewkeg.LatestBackup()
	if err != nil {
		fmt.Printf("Nothing to undo: %v\n", err)
		return 0
	}
	fmt.Println("This restores your configs from backup", b.ID)
	if code := runRestore(append(args, "--backup", b.ID)); code != 0 {
		return code
	}
	// Uninstall is explicit: forget the key too, so it does not sit in
	// ~/.brewkeg/key after the user asked us to leave.
	if err := brewkeg.ClearKey(); err != nil {
		fmt.Fprintf(os.Stderr, "  note: could not clear stored key: %v\n", err)
	}
	return 0
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, name+"=") {
			return strings.TrimPrefix(a, name+"=")
		}
	}
	return ""
}

// runReset is the CLI half of the desktop app's "Reset Gateway…" menu item:
// same engine, same order, same backup — so the two can never disagree about
// what a reset does.
func runReset(args []string) int {
	dry := hasFlag(args, "--dry-run")
	plan := brewkeg.PlanReset(brewkeg.DefaultSpec())

	if len(plan.Targets) == 0 && !plan.Key && !plan.Config && len(plan.Caches) == 0 {
		fmt.Println("Nothing to reset — brewkeg is not configured on this machine.")
		return 0
	}

	fmt.Println()
	fmt.Println("Reset brewkeg")
	fmt.Println()
	for _, id := range plan.Targets {
		fmt.Println("  · disconnect", brewkeg.LabelFor(brewkeg.DefaultSpec(), id))
	}
	if plan.Key {
		fmt.Println("  · forget the stored API key")
	}
	if plan.Config {
		fmt.Println("  · remove", brewkeg.UserConfigPath())
	}
	for _, c := range plan.Caches {
		fmt.Println("  · clear", c)
	}
	if len(plan.Targets) > 0 {
		fmt.Println("\nA backup is saved first, so this can be undone with `brewkeg restore`.")
	}

	if dry {
		fmt.Println("\n(dry run — nothing changed)")
		return 0
	}
	if !confirm("Reset this machine?", false) {
		fmt.Println("\nCancelled. Nothing changed.")
		return 0
	}

	_, b, results, err := brewkeg.Reset(brewkeg.DefaultSpec(), false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n  ✖ %v\n", err)
		return 1
	}
	fmt.Println()
	for _, r := range results {
		if !r.OK {
			fmt.Printf("  ✖ %s — %s\n", r.Label, r.Error)
			continue
		}
		fmt.Printf("  ✔ %s — disconnected\n", r.Label)
	}
	if b != nil {
		fmt.Printf("\nBackup %s saved. Undo with `brewkeg restore`.\n", b.ID)
	}
	fmt.Println("\nDone. Reopen your terminal if it had brewkeg exports.")
	return 0
}
