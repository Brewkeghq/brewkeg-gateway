// Copyright (c) 2026 brewkeg. All rights reserved.
// See LICENSE — this software is proprietary and not licensed for reuse.
// Package main is Brewkeg Gateway, the brewkeg desktop app: paste an API key, pick
// what to turn on, done. It is a thin shell over internal/brewkeg — the same
// engine the CLI uses, so the two can never disagree about what they write.
// The binary is `brewkeg-gateway`; the CLI alongside it stays `brewkeg`.
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
	// `brewkeg-gateway --apply` is the same engine with no window and no questions:
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
		Title:  "Brewkeg Gateway",
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
	fmt.Print(`Brewkeg Gateway — configure your tools to use brewkeg

  brewkeg-gateway         open the window (normal use)
  brewkeg-gateway --apply configure now, no window, from ~/.brewkeg/config.json
  brewkeg-gateway --undo  restore the latest backup, no window
  brewkeg-gateway --version print the version

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
// ToggleDevModeFromMenu is the ⇧⌘D item. Turning developer mode ON is where the
// caution belongs: the next switch the user flips writes the custom gateway URL
// into their real config files, and "I forgot I was on staging" is the kind of
// mistake that costs someone an afternoon. Turning it off just goes back.
func (a *App) ToggleDevModeFromMenu() {
	if brewkeg.DevModeOn() {
		if err := brewkeg.SetDevMode(false); err != nil {
			_, _ = a.info("Could not turn developer mode off: "+err.Error(), "Developer Mode")
			return
		}
		a.emitDevChanged()
		return
	}
	s := brewkeg.ReadDevSettings()
	if s.BaseURL == "" {
		// Nothing to turn on yet — send them to the window, which is the only
		// place a URL can be typed.
		a.emitDevChanged()
		_, _ = a.info("Enter a gateway URL in the window to turn developer mode on.", "Developer Mode")
		return
	}
	if !a.ask("Turn on developer mode?",
		fmt.Sprintf("Brewkeg Gateway will talk to %s instead of brewkeg.dev.\n\nThe next switch you flip writes that URL into your tool configs, and they will keep using it until you turn this off or reset.\n\n%s", s.BaseURL, brewkeg.DevSettingsPath()),
		"Turn On", "Cancel") {
		return
	}
	if err := brewkeg.SetDevMode(true); err != nil {
		_, _ = a.info("Could not turn developer mode on: "+err.Error(), "Developer Mode")
		return
	}
	a.emitDevChanged()
}

func appMenu(a *App) *menu.Menu {
	m := menu.NewMenu()

	app := m.AddSubmenu("Brewkeg Gateway")
	app.AddText("About Brewkeg Gateway", nil, func(_ *menu.CallbackData) {
		_, _ = a.info("Brewkeg Gateway "+brewkeg.Version+"\n© 2026 brewkeg. Proprietary.", "About Brewkeg Gateway")
	})
	app.AddSeparator()
	app.AddText("Get an API Key…", nil, func(_ *menu.CallbackData) { a.OpenDashboard() })
	app.AddText("Check for Updates…", keys.Combo("u", keys.CmdOrCtrlKey, keys.ShiftKey),
		func(_ *menu.CallbackData) { a.CheckForUpdateFromMenu() })
	app.AddSeparator()
	// Developer mode sits above Reset, not below it, because it changes what
	// Reset would clean up. Repointed at a staging gateway, a reset has to evict
	// the staging URL — and a user who cannot see the override will not know
	// which machine they are on at all.
	app.AddCheckbox("Developer Mode", brewkeg.DevModeOn(), keys.Combo("d", keys.CmdOrCtrlKey, keys.ShiftKey),
		func(_ *menu.CallbackData) { a.ToggleDevModeFromMenu() })
	app.AddSeparator()
	// The reset. Not a switch, and deliberately not next to one: it forgets the
	// key as well as the configs, so it asks first and describes what it found.
	app.AddText("Reset Brewkeg Gateway…", keys.Combo("backspace", keys.CmdOrCtrlKey, keys.ShiftKey),
		func(_ *menu.CallbackData) { a.ResetFromMenu() })
	app.AddText("Backups Folder", keys.Combo("b", keys.CmdOrCtrlKey, keys.ShiftKey),
		func(_ *menu.CallbackData) { a.RevealBackupDir() })
	app.AddText("Undo Last Change", keys.Combo("z", keys.CmdOrCtrlKey, keys.ShiftKey),
		func(_ *menu.CallbackData) { a.undoFromMenu() })
	app.AddSeparator()
	app.AddText("Quit Brewkeg Gateway", keys.CmdOrCtrl("q"), func(_ *menu.CallbackData) { runtime.Quit(a.ctx) })

	// Role menus take no items — the OS supplies them.
	m.Append(&menu.MenuItem{Role: menu.EditMenuRole})
	m.Append(&menu.MenuItem{Role: menu.WindowMenuRole})
	return m
}
