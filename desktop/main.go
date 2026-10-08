// Copyright (c) 2026 brewkeg. All rights reserved.
// See LICENSE — this software is proprietary and not licensed for reuse.
// Package main is Gateway, the brewkeg desktop app: paste an API key, pick
// what to turn on, done. It is a thin shell over internal/brewkeg — the same
// engine the CLI uses, so the two can never disagree about what they write.
// The binary is `gateway`; the CLI alongside it stays `brewkeg`.
package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/brewkeghq/brewkeg-gateway/internal/brewkeg"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
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
		Title:  "Gateway",
		Width:  460,
		Height: 560,
		// The key field, four service rows and the footer are the whole
		// window now that there is no primary button. The floor is set by
		// the service rows: below it the labels start truncating.
		MinWidth:  420,
		MinHeight: 470,
		// A real menu bar, not an in-window fake. It carries the actions that
		// are not "pick a service": the Edit menu the key field needs to make
		// ⌘C/⌘V work at all, and the reset that has to sit behind a
		// confirmation rather than next to a switch someone can fat-finger.
		Menu: appMenu(app),
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

// appMenu builds the macOS menu bar.
//
// Edit and Window are declared by ROLE and carry none of our items: macOS owns
// Undo, ⌘C/⌘V and the green button, and an app that lists those itself gets a
// menu that looks right and does nothing. The key field is the first thing
// every user touches, so ⌘C/⌘V has to work — the role is what makes it work.
//
// The application menu is ours, because the reset lives there and the stock one
// has nowhere to put it.
func appMenu(a *App) *menu.Menu {
	m := menu.NewMenu()

	app := m.AddSubmenu("Gateway")
	app.AddText("About Gateway", nil, func(_ *menu.CallbackData) {
		_, _ = a.info("Gateway "+brewkeg.Version+"\nby brewkeg — © 2026, proprietary.", "About Gateway")
	})
	app.AddSeparator()
	app.AddText("Get an API Key…", nil, func(_ *menu.CallbackData) { a.OpenDashboard() })
	app.AddText("Check for Updates…", keys.Combo("u", keys.CmdOrCtrlKey, keys.ShiftKey),
		func(_ *menu.CallbackData) { a.CheckForUpdateFromMenu() })
	app.AddSeparator()
	// The reset. Not a switch, and deliberately not next to one: it forgets the
	// key as well as the configs, so it asks first and describes what it found.
	app.AddText("Reset Gateway…", keys.Combo("backspace", keys.CmdOrCtrlKey, keys.ShiftKey),
		func(_ *menu.CallbackData) { a.ResetFromMenu() })
	app.AddText("Backups Folder", keys.Combo("b", keys.CmdOrCtrlKey, keys.ShiftKey),
		func(_ *menu.CallbackData) { a.RevealBackupDir() })
	app.AddText("Undo Last Change", keys.Combo("z", keys.CmdOrCtrlKey, keys.ShiftKey),
		func(_ *menu.CallbackData) { a.undoFromMenu() })
	app.AddSeparator()
	app.AddText("Quit Gateway", keys.CmdOrCtrl("q"), func(_ *menu.CallbackData) { runtime.Quit(a.ctx) })

	// Role menus take no items — the OS supplies them.
	m.Append(&menu.MenuItem{Role: menu.EditMenuRole})
	m.Append(&menu.MenuItem{Role: menu.WindowMenuRole})
	return m
}
