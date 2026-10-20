// Copyright (c) 2026 brewkeg. All rights reserved.
// See LICENSE — this software is proprietary and not licensed for reuse.
package main

import (
	"fmt"
	"os"

	"github.com/brewkeghq/brewkeg-gateway/internal/brewkeg"
)

const usage = `brewkeg — run coding agents and desktop apps on brewkeg models

  brewkeg                            interactive menu (in a terminal)
  brewkeg login                      save your brewkeg API key
  brewkeg claude   [--model M] [args…]  Claude Code
  brewkeg codex    [--model M] [args…]  Codex
  brewkeg opencode [--model M] [args…]  OpenCode
  brewkeg pi       [--model M] [args…]  Pi
  brewkeg dsh      [--model M] [args…]  DeepSeek Harness
  brewkeg kilo     [--model M] [args…]  Kilo Code
  brewkeg command-code [--model M] [args…]  Command Code (alias: cmd)
  brewkeg models                      live model list with prices
  brewkeg doctor                      check key, connectivity, agent versions and updates
  brewkeg usage [--last 7d] [--json]   your token history and estimated cost
  brewkeg update [--check]            update brewkeg itself to the latest release
  brewkeg desktop status              app and connection status
  brewkeg desktop disconnect --all    restore every connection
  brewkeg uninstall [--yes]           restore connections, remove the key
  brewkeg version

  --print-env   print what would be injected instead of launching
  Everything after the agent name (other than --model / --print-env) is passed through.

Setup and repair:

  brewkeg setup                 Interactive setup (backs up first, restorable)
  brewkeg setup --yes           No prompts: read ~/.brewkeg/config.json
  brewkeg status                Show what is configured
  brewkeg backups               List backups
  brewkeg restore               Undo the most recent setup
  brewkeg restore --backup ID   Undo a specific setup
  brewkeg reset                 Disconnect everything and forget the key

Options:
  --api-key KEY        brewkeg key (bk_live_...)
  --base-url URL       gateway base url (default https://brewkeg.dev)
  --model ID           model the agent runs (default per agent)
  --last RANGE         usage range: 24h, 7d, 30d, 90d (default 7d)
  --backup ID          which backup to restore
  --dry-run            show what restore would do, change nothing
  --skip-check         write config without testing the key first
  -y, --yes            never prompt; take the key and targets from the file

Config file (~/.brewkeg/config.json) — optional, so setup can be scripted:
  { "apiKey": "bk_live_...", "targets": ["claude-cli", "codex"] }
Flags win over the file; the file wins over a prompt.

Every file brewkeg touches is copied to ~/.brewkeg/backups/<id>/ first, so
"brewkeg restore" puts your machine back exactly as it was.

The desktop app is the same engine with a window. It reads the same config
file:  gateway --apply   configures now, no window
                        gateway --undo    restores the latest backup
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(runMenu())
	}

	switch args[0] {
	case "login", "auth":
		os.Exit(runLogin(args[1:]))
	case "models", "model":
		os.Exit(runModels(args[1:]))
	case "doctor":
		os.Exit(runDoctor(args[1:]))
	case "usage":
		os.Exit(runUsage(args[1:]))
	case "update", "upgrade":
		os.Exit(runUpdate(args[1:]))
	case "desktop":
		os.Exit(runDesktop(args[1:]))
	case "setup", "configure", "init":
		os.Exit(runSetup(args[1:]))
	case "restore", "undo":
		os.Exit(runRestore(args[1:]))
	case "backups":
		os.Exit(runBackups())
	case "status":
		os.Exit(runStatus())
	case "reset":
		os.Exit(runReset(args[1:]))
	case "uninstall":
		os.Exit(runUninstall(args[1:]))
	case "version", "--version", "-v":
		fmt.Println("brewkeg " + brewkeg.Version)
		fmt.Println("(c) 2026 brewkeg. Proprietary — see LICENSE.")
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		if agent, ok := brewkeg.AgentByID(args[0]); ok {
			os.Exit(runAgent(agent.ID, args[1:]))
		}
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		fmt.Print(usage)
		os.Exit(2)
	}
}
