package main

import (
	"context"
	"fmt"
	goruntime "runtime"
	"strings"
	"time"

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
	Version   string `json:"version"`
	BaseURL   string `json:"baseUrl"`
	Dashboard string `json:"dashboard"`
	HasKey    bool   `json:"hasKey"`
	MaskedKey string `json:"maskedKey"`
	// ApiKey is the remembered key, sent so the field opens filled in. This is
	// a local desktop app talking to its own window over the Wails bridge; it
	// is the same value already sitting in the user's tool configs.
	ApiKey   string                 `json:"apiKey"`
	Targets  []brewkeg.TargetStatus `json:"targets"`
	Backups  []brewkeg.Backup       `json:"backups"`
	Platform string                 `json:"platform"`
	// StaleCache is a gateway-keyed cache that configuring will clear.
	StaleCache string `json:"staleCache,omitempty"`
	// SpecVersion is the client-config version in force. 0 means the built-in
	// spec, i.e. the server could not be reached or had nothing newer.
	SpecVersion int `json:"specVersion"`
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
		st.ApiKey = k
	}
	if n := brewkeg.StaleGatewayCachesPending(); len(n) > 0 {
		st.StaleCache = n[0]
	}
	st.SpecVersion = brewkeg.DefaultSpec().Version
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
	// Restart lists what the user must reopen for the change to take. A config
	// file is only read at startup, so skipping this looks like we did nothing.
	Restart []brewkeg.RestartHint `json:"restart,omitempty"`
	Message string                `json:"message"`
	// Warning is shown when we went ahead without a usable verdict — the
	// gateway was unreachable, so the key was never actually proven good.
	Warning string `json:"warning,omitempty"`
	// Key is the validation result that gated (or failed to gate) this write.
	Key brewkeg.KeyCheck `json:"key"`
	// Relaunched lists the desktop apps we cycled so the change took effect.
	Relaunched []brewkeg.RelaunchResult `json:"relaunched,omitempty"`
}

// Configure backs up, then writes the selected targets. The backup is saved
// before anything is reported as done, so the window can always offer an undo.
func (a *App) Configure(apiKey string, ids []string, baseURL, mainModel, fastModel string) ConfigureResult {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		// The field opens pre-filled from the key store, so an empty box means
		// the user deliberately cleared it. Fall back to what we remember
		// rather than refusing a change that needs no new secret.
		apiKey = brewkeg.StoredKey()
	}
	if apiKey == "" {
		return ConfigureResult{Message: "Paste your brewkeg API key first."}
	}
	if len(ids) == 0 {
		return ConfigureResult{Message: "Turn on at least one service."}
	}

	// Never write a key we have been told is bad. A rejected key written into
	// ~/.codex/config.toml or ~/.claude/settings.json breaks that tool with an
	// auth error the user has to go and edit by hand — and our own backup
	// cannot save them, because the config they were using is already broken.
	//
	// "Could not reach brewkeg" is NOT a rejection: it says nothing about the
	// key, and blocking on it would make the app unusable offline. That case
	// proceeds with a warning.
	check := brewkeg.CheckKey(a.context(), brewkeg.BaseURL(), apiKey)
	if check.Blocked() {
		return ConfigureResult{Message: check.Message + " Nothing was changed.", Key: check}
	}

	opts := brewkeg.Options{APIKey: apiKey, BaseURL: baseURL, MainModel: mainModel, FastModel: fastModel}
	b, results, err := brewkeg.Apply(ids, opts)
	out := ConfigureResult{BackupID: b.ID, BackupDir: brewkeg.BackupDir(b.ID), Results: results, Key: check}
	if !check.Reachable {
		out.Warning = "Could not test the key — brewkeg did not answer. Saved anyway."
	}
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

	var paths []string
	for _, r := range results {
		if r.OK {
			paths = append(paths, r.Paths...)
		}
	}
	out.Restart = brewkeg.RestartHintsFor(ids, paths)

	// Now do it rather than printing a list of things the user will forget to
	// do. Only apps that were already running are touched.
	out.Relaunched = brewkeg.Relaunch(brewkeg.RestartAppsFor(ids), 8*time.Second)

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

// RefreshSpec asks the gateway what to write for each tool. The app renders
// from the built-in spec immediately and calls this in the background, so a
// change to how a tool stores its config reaches users without a new release.
func (a *App) RefreshSpec() (brewkeg.Spec, error) {
	return brewkeg.FetchSpec(a.context(), brewkeg.BaseURL())
}

// CheckKey tests an API key against the live gateway without writing anything,
// so a typo never reaches the user's config files.
func (a *App) CheckKey(apiKey string) brewkeg.KeyCheck {
	return brewkeg.CheckKey(a.context(), brewkeg.BaseURL(), apiKey)
}

// CopyToClipboard puts text on the system clipboard, so the restart list can be
// pasted rather than retyped.
func (a *App) CopyToClipboard(text string) {
	runtime.ClipboardSetText(a.ctx, text)
}

// OpenRepo sends the user to the source. The app edits their dotfiles and makes
// live network calls, so the code has to be one click away from the window.
func (a *App) OpenRepo() {
	runtime.BrowserOpenURL(a.ctx, brewkeg.RepoURL)
}
