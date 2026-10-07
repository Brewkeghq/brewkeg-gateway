package main

import (
	"context"
	"fmt"
	goruntime "runtime"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/brewkeg/brewkeg-cli/internal/brewkeg"
)

// App is the binding surface the frontend calls. Every method returns plain
// structs or strings so the JS side stays simple.
type App struct {
	ctx context.Context
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// State is everything the window needs on open, in one round trip.
type State struct {
	Version   string                 `json:"version"`
	BaseURL   string                 `json:"baseUrl"`
	Dashboard string                 `json:"dashboard"`
	HasKey    bool                   `json:"hasKey"`
	MaskedKey string                 `json:"maskedKey"`
	Targets   []brewkeg.TargetStatus `json:"targets"`
	Backups   []brewkeg.Backup       `json:"backups"`
	Platform  string                 `json:"platform"`
}

func (a *App) GetState() State {
	st := State{
		Version:   brewkeg.Version,
		BaseURL:   brewkeg.BaseURL(),
		Dashboard: brewkeg.BaseURL() + "/dashboard",
		Targets:   brewkeg.StatusAll(),
		Platform:  platform(),
	}
	if k := brewkeg.StoredKey(); k != "" {
		st.HasKey = true
		st.MaskedKey = brewkeg.MaskKey(k)
	}
	if all, err := brewkeg.ListBackups(); err == nil {
		st.Backups = all
	}
	return st
}

func platform() string { return goruntime.GOOS + "/" + goruntime.GOARCH }

// ConfigureResult is what the window shows after the big button is pressed.
type ConfigureResult struct {
	OK        bool                  `json:"ok"`
	BackupID  string                `json:"backupId"`
	BackupDir string                `json:"backupDir"`
	Results   []brewkeg.ApplyResult `json:"results"`
	Message   string                `json:"message"`
}

// Configure backs up, then writes the selected targets. The backup is saved
// before anything is reported as done, so the window can always offer an undo.
func (a *App) Configure(apiKey string, ids []string, baseURL, mainModel, fastModel string) ConfigureResult {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return ConfigureResult{Message: "Paste your brewkeg API key first."}
	}
	if len(ids) == 0 {
		return ConfigureResult{Message: "Turn on at least one service."}
	}

	opts := brewkeg.Options{APIKey: apiKey, BaseURL: baseURL, MainModel: mainModel, FastModel: fastModel}
	b, results, err := brewkeg.Apply(ids, opts)
	out := ConfigureResult{BackupID: b.ID, BackupDir: brewkeg.BackupDir(b.ID), Results: results}
	if err != nil {
		out.Message = fmt.Sprintf("Backup could not be saved: %v", err)
		return out
	}

	failed := 0
	for _, r := range results {
		if !r.OK {
			failed++
		}
	}
	if failed == len(results) {
		out.Message = "Nothing could be configured. Your files are untouched."
		return out
	}
	out.OK = true
	n := len(results) - failed
	if failed > 0 {
		out.Message = fmt.Sprintf("Configured %d of %d services. %d failed — see details.", n, len(results), failed)
	} else {
		out.Message = fmt.Sprintf("Configured %s. A backup was saved first.", plural(n))
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return "1 service"
	}
	return fmt.Sprintf("%d services", n)
}

// Restore puts one backup back the way it was.
func (a *App) Restore(id string) (string, error) {
	if id == "" {
		b, err := brewkeg.LatestBackup()
		if err != nil {
			return "", err
		}
		id = b.ID
	}
	b, err := brewkeg.LoadBackup(id)
	if err != nil {
		return "", err
	}
	results, err := b.Restore(false)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, r := range results {
		lines = append(lines, r.Path+" — "+r.Action)
	}
	return strings.Join(lines, "\n"), nil
}

// OpenDashboard sends the user to where the key comes from, so "I don't have a
// key yet" has one obvious next step instead of a dead end.
func (a *App) OpenDashboard() {
	runtime.BrowserOpenURL(a.ctx, brewkeg.BaseURL()+"/dashboard")
}

// RevealBackupDir opens the folder holding the backups, so a user can keep
// their own copy of their configs.
func (a *App) RevealBackupDir() {
	runtime.BrowserOpenURL(a.ctx, "file://"+brewkeg.BackupsRoot())
}

// CheckKey tests an API key against the live gateway without writing anything,
// so a typo never reaches the user's config files.
func (a *App) CheckKey(apiKey string) brewkeg.KeyCheck {
	return brewkeg.CheckKey(a.context(), brewkeg.BaseURL(), apiKey)
}

// OpenRepo sends the user to the source. The app edits their dotfiles and makes
// live network calls, so the code has to be one click away from the window.
func (a *App) OpenRepo() {
	runtime.BrowserOpenURL(a.ctx, brewkeg.RepoURL)
}
