// Package main is Gateway, the brewkeg desktop app: paste an API key, pick
// what to turn on, done. It is a thin shell over internal/brewkeg — the same
// engine the CLI uses, so the two can never disagree about what they write.
// The binary is `gateway`; the CLI alongside it stays `brewkeg`.
package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/brewkeg/brewkeg-cli/internal/brewkeg"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	// `gateway --apply` is the same engine with no window and no questions:
	// it reads ~/.brewkeg/config.json and writes. That exists so provisioning
	// a machine does not mean clicking through menus, and so the app and the
	// CLI can never disagree about what they would write.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--apply", "--headless":
			os.Exit(headless())
		case "--undo":
			os.Exit(headlessUndo())
		case "--version", "-v":
			fmt.Println(brewkeg.Version)
			os.Exit(0)
		case "--help", "-h":
			headlessHelp()
			os.Exit(0)
		}
	}

	app := NewApp()
	err := wails.Run(&options.App{
		Title:     "Gateway",
		Width:     600,
		Height:    580,
		MinWidth:  520,
		MinHeight: 460,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: app.startup,
		Bind:      []interface{}{app},
	})
	if err != nil {
		fmt.Println(err)
	}
}

// headlessHelp is only for the flag forms — with no arguments the app opens its
// window, which is the normal way to use it.
func headlessHelp() {
	fmt.Print(`Gateway — configure your tools to use brewkeg

  gateway                 open the window (normal use)
  gateway --apply         configure now, no window, from ~/.brewkeg/config.json
  gateway --undo          restore the latest backup, no window
  gateway --version       print the version

The config file lives at ` + brewkeg.UserConfigPath() + `:

  {
    "apiKey": "bk_live_...",
    "targets": ["claude-cli", "codex"]
  }

Undo a window session from the app's Undo button, or with: brewkeg restore
`)
}
