package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/brewkeghq/brewkeg-gateway/internal/brewkeg"
)

// checklistItems turns the target statuses into the rows the prompt draws.
//
// The tick means "brewkeg will manage this", and it starts on what brewkeg
// already has — not on what merely happens to be installed. Pre-ticking every
// tool found on the machine made the list read as a list of things already
// done: the user pressed space on the one tool they actually wanted, which
// turned it OFF, and the tools they never touched were the ones configured.
//
// So the tick starts on the connected tools (a re-run leaves them alone) and
// adding a tool means ticking it.
func checklistItems(status []brewkeg.TargetStatus) []choice {
	var items []choice
	for _, s := range status {
		hint := "not detected — will write config anyway"
		if s.Installed {
			hint = "found"
		}
		if s.Enabled {
			hint = "found — already connected"
		}
		items = append(items, choice{label: s.Label, icon: s.Icon, checked: s.Enabled, hint: hint})
	}
	return items
}

func runSetup(args []string) int {
	spec, _ := brewkeg.FetchSpec(context.Background(), brewkeg.UserBaseURL())
	status := brewkeg.StatusAllFrom(spec)

	var opts brewkeg.Options
	opts.APIKey = flagValue(args, "--api-key")
	opts.BaseURL = flagValue(args, "--base-url")
	opts.MainModel = flagValue(args, "--model")
	opts.SonnetModel = flagValue(args, "--sonnet-model")
	opts.FastModel = flagValue(args, "--fast-model")
	opts.CodexModel = flagValue(args, "--codex-model")

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

	items := checklistItems(status)

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
			//
			// It is also the default now that nothing is pre-ticked, so it
			// asks before it takes everything out.
			if !brewkeg.AnyEnabled() {
				fmt.Println("\nNothing selected. Nothing changed.")
				return 1
			}
			if !confirm("No services selected — disconnect brewkeg from every tool it is in?", false) {
				fmt.Println("\nNothing was changed.")
				return 1
			}
			fmt.Println("\nDisconnecting everything brewkeg currently points at.")
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
		opts.APIKey = promptSecret("Enter your brewkeg API key (from brewkeg.dev/dashboard/keys)")
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
		res := brewkeg.CheckKey(context.Background(), opts.BaseURL, opts.APIKey)
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
			// Both, when there is both: the file we wrote, and the step the
			// app still needs from the user. Printing only "configured by
			// hand" hides the fact that the file was already written.
			if len(r.Paths) > 0 {
				fmt.Printf("  ✔ %s → %s\n", r.Label, strings.Join(r.Paths, " + "))
			} else {
				fmt.Printf("  ✔ %s is configured by hand in the app:\n", r.Label)
			}
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

	// Restart what we changed, so the user does not have to.
	//
	// A config file is read once, at launch, so writing it changes nothing
	// until the app comes back. Telling the user to quit and reopen by hand
	// works exactly once — the second time they forget, and the app looks
	// broken while being perfectly configured. The MCP server is started by
	// the app, so without a restart it never starts at all.
	restarted := map[string]bool{}
	for _, r := range brewkeg.Relaunch(brewkeg.RestartAppsFor(changedTargets(results)), 8*time.Second) {
		switch r.Action {
		case "restarted":
			restarted[r.App] = true
			fmt.Printf("  ✔ %s restarted — it has the new configuration now\n", r.App)
		case "not running":
			// Nothing to do: the app was closed, and it will read the new
			// configuration when the user opens it. Telling them to reopen it
			// would be asking them to do something they have not started.
			restarted[r.App] = true
		case "could not restart":
			fmt.Fprintf(os.Stderr, "  ✖ %s could not be restarted: %s\n", r.App, r.Detail)
		}
	}

	// The marketplace and the MCP server only work if the gateway is serving
	// them. Say so here, where the user is already looking, rather than
	// letting them find an empty plugin tab later and blame the setup.
	if configured(results, "desktop") {
		opts = opts.WithDefaults()
		st := brewkeg.CheckMarketplace(context.Background(), opts.BaseURL, opts.APIKey)
		if st.Served {
			fmt.Printf("  ✔ Plugin marketplace: %s\n", st.Message)
		} else {
			fmt.Fprintf(os.Stderr, "  ⚠ %s\n", st.Message)
			fmt.Fprintf(os.Stderr, "    Claude Desktop's plugin tab stays empty until the gateway serves it.\n")
		}
		fmt.Println("  ✔ MCP server: brewkeg mcp (models, usage, plans) — Claude Desktop starts it")
		if n := brewkeg.MarketplaceCount(spec); n > 1 {
			fmt.Printf("  ✔ Plugin catalogue: brewkeg, plus %d public marketplaces\n", n-1)
		} else {
			fmt.Println("  ✔ Plugin catalogue: brewkeg")
		}
		fmt.Println("    (Figma, Miro, Canva, Notion, Linear, Slack, Sentry, Vercel and the rest)")
		if brewkeg.LookBin("git") == "" {
			// The app clones those marketplaces with git. It will say so
			// itself, but saying it here means the user is not left
			// wondering why the tab is half empty.
			fmt.Println("  ⚠ git is not installed — the public marketplaces need it. The app will say so too.")
		}
	}

	if hints := restartHints(picked, pathsOf(results), restarted); len(hints) > 0 {
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

// changedTargets are the targets this run actually wrote to. An app that was
// already configured to exactly these values is not restarted: restarting it
// for nothing is the kind of thing that teaches people to distrust the tool.
func changedTargets(results []brewkeg.ApplyResult) []string {
	var out []string
	for _, r := range results {
		if r.OK && r.Changed {
			out = append(out, r.ID)
		}
	}
	return out
}

// configured reports whether a target is now pointed at brewkeg.
func configured(results []brewkeg.ApplyResult, id string) bool {
	for _, r := range results {
		if r.ID == id && r.OK && !r.Removed {
			return true
		}
	}
	return false
}

// restartHints drops the hint for an app we restarted ourselves: telling
// someone to quit and reopen an app that is already open is noise.
func restartHints(ids []string, changedPaths []string, restarted map[string]bool) []brewkeg.RestartHint {
	var out []brewkeg.RestartHint
	for _, h := range brewkeg.RestartHintsFor(ids, changedPaths) {
		if restarted[h.What] {
			continue
		}
		out = append(out, h)
	}
	return out
}

func looksLikeKey(k string) bool {
	l := strings.ToLower(k)
	return strings.HasPrefix(l, "bk_live") || strings.HasPrefix(l, "bk_test") || strings.HasPrefix(l, "sk-")
}
