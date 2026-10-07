// Package main is the brewkeg desktop app: paste an API key, pick what to
// turn on, done. It is a thin shell over internal/brewkeg — the same engine
// the CLI uses, so the two can never disagree about what they write.
package main

import (
	"embed"
	"fmt"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:  "brewkeg",
		Width:  600,
		Height: 580,
		MinWidth:  520,
		MinHeight: 460,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: app.startup,
		Bind:      []interface{}{app},
		Mac:       &mac.Options{TitleBar: mac.TitleBarHiddenInset()},
	})
	if err != nil {
		fmt.Println(err)
	}
}
