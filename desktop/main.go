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
// The Edit menu is not decoration: without it ⌘C and ⌘V do nothing in the API
// key field, which is the first thing every user does. The Gateway menu holds
// the actions that are about this machine rather than about one service.
func appMenu(a *App) *menu.Menu {
	return menu.NewMenu(
		// macOS requires the first menu to be the application menu or the app
		// name never appears in the bar.
		&menu.Submenu{
			Label: "Gateway",
			Items: []*menu.Item{
				{Label: "About Gateway", Type: menu.AboutType},
				{Type: menu.SeparatorType},
				{
					Label:    "Get an API key…",
					Type:     menu.OpenDirectoryType,
					Platforms: []string{"darwin"},
				},
				{
					Label:     "Check for Updates…",
					Accelerator: keys.New(keys.Cmd(), keys.Shift(), keys.Key("u")),
					Action:     func(_ *menu.CallbackData) { a.checkForUpdateFromMenu() },
				},
				{Type: menu.SeparatorType},
				{
					Label:     "Reset Gateway…",
					Accelerator: keys.New(keys.Cmd(), keys.Shift(), keys.Backspace),
					Action:     func(_ *menu.CallbackData) { a.ResetFromMenu() },
				},
				{Type: menu.SeparatorType},
				{Role: macAppQuitRole()},
			},
		},
		&menu.Submenu{
			Label: "File",
			Items: []*menu.Item{
				{
					Label:     "Backups folder",
					Accelerator: keys.New(keys.Cmd(), keys.Shift(), keys.Key("b")),
					Action:     func(_ *menu.CallbackData) { a.RevealBackupDir() },
				},
				{
					Label: "Undo last change",
					Action: func(_ *menu.CallbackData) { a.undoFromMenu() },
				},
			},
		},
		&menu.Submenu{
			Label: "Edit",
			Items: []*menu.Item{
				{Role: macEditRoles("Undo")},
				{Role: macEditRoles("Redo")},
				{Type: menu.SeparatorType},
				{Role: macEditRoles("Cut")},
				{Role: macEditRoles("Copy")},
				{Role: macEditRoles("Paste")},
				{Role: macEditRoles("SelectAll")},
			},
		},
		&menu.Submenu{
			Label: "Window",
			Items: []*menu.Item{
				{Role: macWindowRoles("Minimize")},
				{Role: macWindowRoles("Zoom")},
				{Role: macWindowRoles("Front")},
			},
		},
	)
}
