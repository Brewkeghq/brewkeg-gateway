package main

import (
	"fmt"
	"os"

	"github.com/brewkeg/brewkeg-cli/internal/brewkeg"
)

const usage = `brewkeg — configure Claude Code, Codex and Claude Desktop to use brewkeg

Usage:
  brewkeg setup                 Interactive setup (backs up first, restorable)
  brewkeg setup --api-key KEY   Non-interactive
  brewkeg status                Show what is configured
  brewkeg backups               List backups
  brewkeg restore               Undo the most recent setup
  brewkeg restore --backup ID   Undo a specific setup
  brewkeg uninstall             Alias for restore

Options:
  --api-key KEY        brewkeg key (bk_live_...)
  --base-url URL       gateway base url (default https://brewkeg.dev)
  --model ID           main model for Claude Code (default claude-opus-5)
  --fast-model ID      small/fast model (default claude-haiku-4-5)
  --backup ID          which backup to restore
  --dry-run            show what restore would do, change nothing
  --skip-check         write config without testing the key first

Every file brewkeg touches is copied to ~/.brewkeg/backups/<id>/ first, so
"brewkeg restore" puts your machine back exactly as it was.
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Print(usage)
		os.Exit(0)
	}

	switch args[0] {
	case "setup", "configure", "init":
		os.Exit(runSetup(args[1:]))
	case "restore", "undo":
		os.Exit(runRestore(args[1:]))
	case "backups":
		os.Exit(runBackups())
	case "status", "doctor":
		os.Exit(runStatus())
	case "uninstall":
		os.Exit(runUninstall(args[1:]))
	case "version", "--version", "-v":
		fmt.Println("brewkeg " + brewkeg.Version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		fmt.Print(usage)
		os.Exit(2)
	}
}
