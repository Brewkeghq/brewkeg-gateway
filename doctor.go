package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/brewkeghq/brewkeg-gateway/internal/brewkeg"
)

// runDoctor reports the key, the agents and the desktop connections.
func runDoctor(args []string) int {
	fmt.Println("brewkeg doctor")
	fmt.Println()

	key := resolveKey()
	if key == "" {
		fmt.Println("✗ API key missing — run `brewkeg login`")
	} else {
		res := checkKey(context.Background(), brewkeg.BaseURL(), key)
		switch {
		case res.Rejected:
			fmt.Printf("✗ API key rejected (%s) — run `brewkeg login`\n", res.Message)
		case !res.Reachable:
			fmt.Printf("! API key untested — could not reach %s\n", brewkeg.BaseURL())
		case res.Accepted:
			fmt.Printf("✓ API key accepted (%s)\n", brewkeg.MaskKey(key))
		default:
			fmt.Printf("! API key: %s\n", res.Message)
		}
	}

	fmt.Println()
	for _, a := range brewkeg.Agents() {
		printAgent(a)
	}

	fmt.Println()
	printDesktopStatus()
	return 0
}

// printAgent reports one agent: installed, version, and whether a newer one
// exists.
func printAgent(a brewkeg.Agent) {
	path := brewkeg.LookBin(a.Bin)
	if path == "" {
		fmt.Printf("✗ %s not installed: %s\n", a.Label, a.Install)
		return
	}
	version := brewkeg.AgentVersion(context.Background(), path)
	latest := latestNpmVersion(a.Pkg)

	switch {
	case latest == "":
		fmt.Printf("✓ %s  %s\n", a.Label, version)
	case version == "":
		fmt.Printf("✓ %s  (latest %s)\n", a.Label, latest)
	case newerVersion(latest, version):
		fmt.Printf("↑ %s  %s — %s available\n", a.Label, version, latest)
		if a.Update != "" {
			fmt.Printf("  update: %s\n", a.Update)
		}
	default:
		fmt.Printf("✓ %s  %s (up to date)\n", a.Label, version)
	}
	fmt.Printf("  %s\n", path)
}

// desktopApp is one desktop application brewkeg can be connected to.
type desktopApp struct {
	Name      string
	Paths     []string
	Config    string
	Connected bool
	Note      string
}

// desktopApps are the desktop applications, and how to tell whether brewkeg
// is connected to each.
func desktopApps() []desktopApp {
	claude := desktopApp{
		Name:   "Claude Desktop",
		Paths:  claudeAppPaths(),
		Config: brewkeg.ClaudeDesktopAppliedConfig(),
	}
	if claude.Config == "" {
		claude.Config = brewkeg.ClaudeDesktopSupportDir()
	}
	claude.Connected = claudeDesktopConnected()

	codex := desktopApp{
		Name:      "Codex Desktop",
		Paths:     codexAppPaths(),
		Config:    brewkeg.HomeJoin(".codex", "config.toml"),
		Connected: brewkeg.CodexPointsAtBrewkeg(brewkeg.ReadFile(brewkeg.HomeJoin(".codex", "config.toml"))),
		Note:      "shares config.toml with plain codex",
	}
	return []desktopApp{codex, claude}
}

func claudeAppPaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/Applications/Claude.app", brewkeg.HomeJoin("Applications", "Claude.app")}
	case "windows":
		return []string{filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Claude", "Claude.exe")}
	}
	return []string{"/opt/Claude", "/usr/bin/claude-desktop"}
}

func codexAppPaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/Applications/ChatGPT.app", brewkeg.HomeJoin("Applications", "ChatGPT.app")}
	case "windows":
		return []string{filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "ChatGPT", "ChatGPT.exe")}
	}
	return []string{"/opt/ChatGPT", "/usr/bin/chatgpt"}
}

// claudeDesktopConnected reports whether the applied Claude Desktop inference
// configuration points at brewkeg.
func claudeDesktopConnected() bool {
	path := brewkeg.ClaudeDesktopAppliedConfig()
	if path == "" {
		return false
	}
	raw := brewkeg.ReadFile(path)
	return strings.Contains(raw, "brewkeg")
}

// printDesktopStatus reports each desktop app and its connection.
func printDesktopStatus() {
	apps := desktopApps()
	connected := 0
	for _, a := range apps {
		if a.Connected {
			connected++
		}
	}
	if connected == 0 {
		fmt.Println("Desktop: not configured")
	} else {
		fmt.Printf("Desktop: %d of %d connected\n", connected, len(apps))
	}
	for _, a := range apps {
		state := "Not connected"
		if a.Connected {
			state = "Connected"
		}
		installed := ""
		for _, p := range a.Paths {
			if brewkeg.DirExists(p) || brewkeg.FileExists(p) {
				installed = p
				break
			}
		}
		if installed == "" {
			installed = "not installed"
		}
		fmt.Printf("%s: %s · %s\n", a.Name, installed, state)
		fmt.Printf("  config: %s\n", a.Config)
		if a.Note != "" {
			fmt.Printf("  (%s — not proof of desktop installation)\n", a.Note)
		}
	}
	fmt.Println("\nReopen the apps after a connection changes.")
}

// runDesktop handles `brewkeg desktop status` and `brewkeg desktop disconnect`.
func runDesktop(args []string) int {
	if len(args) == 0 {
		fmt.Println("Usage:")
		fmt.Println("  brewkeg desktop status           app and connection status")
		fmt.Println("  brewkeg desktop disconnect --all restore every connection")
		return 2
	}
	switch args[0] {
	case "status":
		printDesktopStatus()
		return 0
	case "disconnect":
		return desktopDisconnect(args[1:])
	}
	fmt.Fprintf(os.Stderr, "unknown desktop command %q\n", args[0])
	return 2
}

// desktopDisconnect takes brewkeg out of every tool it is currently inside.
func desktopDisconnect(args []string) int {
	if !hasFlag(args, "--all") {
		fmt.Fprintln(os.Stderr, "brewkeg: disconnect needs --all")
		return 2
	}
	spec, _ := brewkeg.FetchSpec(context.Background(), brewkeg.BaseURL())
	plan := brewkeg.PlanReset(spec)
	if len(plan.Targets) == 0 {
		fmt.Println("Nothing to disconnect — brewkeg is not connected on this machine.")
		return 0
	}
	if !confirm("Disconnect brewkeg from every tool?", false) {
		fmt.Println("Cancelled. Nothing changed.")
		return 1
	}

	_, results, err := brewkeg.ApplyWithSpec(spec, []string{}, brewkeg.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "brewkeg: %v\n", err)
		return 1
	}
	fmt.Println()
	for _, r := range results {
		if !r.OK {
			fmt.Printf("  ✖ %s: %s\n", r.Label, r.Error)
			continue
		}
		fmt.Printf("  ✔ %s disconnected\n", r.Label)
	}
	fmt.Println("\nReopen the apps so they stop using the old connection.")
	return 0
}
