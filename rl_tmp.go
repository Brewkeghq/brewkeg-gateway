package main

import (
	"fmt"
	"time"

	"github.com/brewkeg/brewkeg-cli/internal/brewkeg"
)

func main() {
	res := brewkeg.Relaunch([]brewkeg.RelaunchApp{
		{BundleID: "com.anthropic.claudefordesktop", Name: "Claude Desktop"},
		{BundleID: "com.openai.codex", Name: "Codex desktop"},
		{BundleID: "com.example.nope", Name: "Not Installed"},
	}, 8*time.Second)
	for _, r := range res {
		fmt.Printf("%-18s %-18s %s\n", r.App, r.Action, r.Detail)
	}
}
