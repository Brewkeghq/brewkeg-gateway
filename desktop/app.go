package main

import (
	"context"
	"fmt"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/brewkeghq/brewkeg-gateway/internal/brewkeg"
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
		Dashboard: brewkeg.BaseURL() + "/dashboard/keys",
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

// Configure backs up, then writes the selected targets and removes brewkeg from
// the ones that were deselected. The backup is saved before anything is reported
// as done, so the window can always offer an undo.
func (a *App) Configure(apiKey string, ids []string, baseURL, mainModel, sonnetModel, fastModel string) ConfigureResult {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		// The field opens pre-filled from the key store, so an empty box means
		// the user deliberately cleared it. Fall back to what we remember
		// rather than refusing a change that needs no new secret.
		apiKey = brewkeg.StoredKey()
	}

	// Turning everything off is a legitimate request — it disconnects the tools
	// that are currently on. It carries no key with it, because there is
	// nothing left to authenticate against once every tool is pointed away
	// from us. Refusing it for want of a key would make "unconfigure" the one
	// action the app cannot perform.
	if apiKey == "" && !brewkeg.AnyEnabled() {
		return ConfigureResult{Message: "Paste your brewkeg API key first."}
	}

	// Never write a key we have been told is bad. A rejected key written into
	// ~/.codex/config.toml or ~/.claude/settings.json breaks that tool with an
	// auth error the user has to go and edit by hand — and our own backup
	// cannot save them, because the config they were using is already broken.
	//
	// "Could not reach brewkeg" is NOT a rejection: it says nothing about the
	// key, and blocking on it would make the app unusable offline. That case
	// proceeds with a warning.
	var check brewkeg.KeyCheck
	if apiKey != "" {
		check = brewkeg.CheckKey(a.context(), brewkeg.BaseURL(), apiKey)
		if check.Blocked() {
			return ConfigureResult{Message: check.Message + " Nothing was changed.", Key: check}
		}
	}

	opts := brewkeg.Options{APIKey: apiKey, BaseURL: baseURL, MainModel: mainModel, SonnetModel: sonnetModel, FastModel: fastModel}
	b, results, err := brewkeg.Apply(ids, opts)
	out := ConfigureResult{BackupID: b.ID, BackupDir: brewkeg.BackupDir(b.ID), Results: results, Key: check}
	// Only warn about an untested key when there was a key to test. A pure
	// disconnect has no credential in it, so an unreachable gateway is not
	// something the user needs to be told about.
	if apiKey != "" && !check.Reachable {
		out.Warning = "Could not test the key — brewkeg did not answer. Saved anyway."
	}
	if err != nil {
		out.Message = fmt.Sprintf("Backup could not be saved: %v", err)
		return out
	}

	failed := 0
	added := 0
	removed := 0
	for _, r := range results {
		switch {
		case !r.OK:
			failed++
		case !r.Changed:
			// Already exactly as we would have written it.
		case r.Removed:
			removed++
		default:
			added++
		}
	}
	if failed == len(results) {
		out.Message = "Nothing could be configured. Your files are untouched."
		return out
	}
	out.OK = true

	// Only the paths that moved, for the same reason as the restart list below:
	// handing back a docket that lists every service on the machine reads as
	// "we just rewrote all of these".
	var paths []string
	for _, r := range results {
		if r.OK && r.Changed {
			paths = append(paths, r.Paths...)
		}
	}
	// A target that came back with manual steps is not finished, and the user is
	// about to go into that app and finish it themselves. Bouncing it for the
	// half we could do — on Claude Desktop that is only `allowDevTools`, while
	// the gateway profile needs a signed-in Developer menu we cannot reach —
	// presents as "it restarted but nothing happened", which is exactly what
	// happened. The steps already end with a quit and reopen.
	touched := finished(results)
	out.Restart = brewkeg.RestartHintsFor(touched, paths)

	// Now do it rather than printing a list of things the user will forget to
	// do. Only apps that were already running are touched.
	out.Relaunched = brewkeg.Relaunch(brewkeg.RestartAppsFor(touched), 8*time.Second)

	out.Message = summary(added, removed, failed)
	return out
}

// finished is the ids we both wrote to and left nothing for the user to do by
// hand. A target can succeed with nothing written — Claude Desktop's gateway
// profile is a manual path until the user creates it — and one can succeed with
// a prerequisite written but the real work still manual. Neither is ours to
// bounce.
//
// Changed is the load-bearing part. The window sends the whole desired set on
// every toggle, so a run for one service rewrites the others too; without this
// filter each of those reported itself as finished and every running app was
// quit and relaunched. Only a target whose bytes actually moved is ours to
// bounce.
func finished(results []brewkeg.ApplyResult) []string {
	var out []string
	for _, r := range results {
		if r.OK && r.Changed && len(r.Paths) > 0 && r.Manual == "" {
			out = append(out, r.ID)
		}
	}
	return out
}

// summary says what happened, in the order a reader cares: what was connected,
// what was disconnected, what failed.
func summary(added, removed, failed int) string {
	var parts []string
	if added > 0 {
		parts = append(parts, fmt.Sprintf("Connected %s", plural(added)))
	}
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("Disconnected %s", plural(removed)))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed — see details", failed))
	}
	return strings.Join(parts, ". ") + ". A backup was saved first."
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
	runtime.BrowserOpenURL(a.ctx, brewkeg.BaseURL()+"/dashboard/keys")
}

// RevealBackupDir opens the folder holding the backups, so a user can keep
// their own copy of their configs.
func (a *App) RevealBackupDir() {
	runtime.BrowserOpenURL(a.ctx, "file://"+brewkeg.BackupsRoot())
}

// ResetResult is what a reset actually did, in the same shape the window uses
// for every other outcome: what changed, where the undo lives, and one honest
// sentence.
type ResetResult struct {
	Message  string                `json:"message"`
	BackupID string                `json:"backupId,omitempty"`
	Results  []brewkeg.ApplyResult `json:"results"`
	Reset    bool                  `json:"reset"`
}

// Reset returns the machine to its pre-brewkeg state: disconnect every service
// brewkeg is inside, forget the stored key, drop config.json, clear the
// gateway-keyed caches. One backup covers the whole thing, so it is undoable.
//
// dryRun reports the plan and writes nothing. The menu item uses it to describe
// the action before the confirmation, rather than guessing what is configured.
func (a *App) Reset(dryRun bool) ResetResult {
	spec := brewkeg.DefaultSpec()
	if fetched, err := brewkeg.FetchSpec(a.context(), brewkeg.BaseURL()); err == nil && len(fetched.Targets) > 0 {
		spec = fetched
	}
	plan, b, results, err := brewkeg.Reset(spec, dryRun)
	out := ResetResult{Results: results, Reset: !dryRun}
	if b != nil {
		out.BackupID = b.ID
	}
	if dryRun {
		out.Message = brewkeg.ResetSummary(plan, results)
		return out
	}
	if err != nil {
		out.Message = "Reset stopped: " + err.Error()
		return out
	}
	out.Message = brewkeg.ResetSummary(plan, results) + " A backup was saved first."
	return out
}

// ResetFromMenu is the menu item. A destructive action behind one click is not
// an action; this describes what will change, using the same plan the run
// acts on, and only proceeds if the user says yes.
func (a *App) ResetFromMenu() {
	plan := a.Reset(true)
	var lines []string
	if len(plan.Results) == 0 && plan.Message == "" {
		lines = append(lines, "Nothing is configured — nothing would change.")
	} else if len(plan.Results) > 0 {
		names := make([]string, 0, len(plan.Results))
		for _, r := range plan.Results {
			names = append(names, r.Label)
		}
		lines = append(lines, "Disconnect: "+strings.Join(names, ", ")+".")
	}
	if plan.BackupID != "" {
		lines = append(lines, "\nA backup is saved first, so this can be undone.")
	}
	if !a.ask("Reset Brewkeg Gateway?", strings.Join(lines, "\n"), "Reset", "Cancel") {
		return
	}
	res := a.Reset(false)
	_, _ = a.info(res.Message, "Reset Brewkeg Gateway")
}

// ask and info are thin wrappers so every dialog in the app goes through one
// pair of calls — MessageDialog answers with the LABEL of the button the user
// clicked, so the comparison has to be made against the label we asked with.
// Getting that wrong silently makes Cancel the destructive one.
func (a *App) ask(title, message, confirm, cancel string) bool {
	got, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         title,
		Message:       message,
		Buttons:       []string{confirm, cancel},
		DefaultButton: cancel,
		CancelButton:  cancel,
	})
	if err != nil {
		return false
	}
	return got == confirm
}

func (a *App) info(message, title string) (string, error) {
	return runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   title,
		Message: message,
		Buttons: []string{"OK"},
	})
}

// undoFromMenu is the menu route to the same undo the docket button offers. It
// asks first: restoring a backup overwrites files, and the menu is one click
// away from the keyboard shortcut.
func (a *App) undoFromMenu() {
	b, err := brewkeg.LatestBackup()
	if err != nil {
		_, _ = a.info("Nothing to undo: "+err.Error(), "Undo Last Change")
		return
	}
	n := len(b.Entries)
	if !a.ask("Undo last change?",
		fmt.Sprintf("Restore backup %s?\n\n%d file(s) go back to how they were before that run.", b.ID, n),
		"Undo", "Cancel") {
		return
	}
	if _, err := a.Restore(b.ID); err != nil {
		_, _ = a.info("Could not restore: "+err.Error(), "Undo Last Change")
	}
}

// CheckForUpdateFromMenu reports the result in a dialog rather than only in the
// window's banner, so a menu invocation is never silent.
func (a *App) CheckForUpdateFromMenu() {
	info := a.CheckForUpdate()
	if !info.Available {
		_, _ = a.info("Brewkeg Gateway "+info.Current+" is the latest version.", "Check for Updates")
		return
	}
	msg := "Brewkeg Gateway " + info.Latest + " is available.\n\nYou are on " + info.Current + "."
	if info.Notes != "" {
		msg += "\n\n" + info.Notes
	}
	if a.ask("Update available", msg, "Open Releases", "Not now") {
		a.OpenUpdate(info.URL)
	}
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
