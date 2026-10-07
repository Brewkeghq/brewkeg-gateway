package brewkeg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// KeyStorePath is where brewkeg remembers the key the user gave us, so neither
// the app nor the CLI has to ask again on next launch.
//
// It used to live only inside ~/.claude/settings.json, which meant a Codex-only
// setup had nowhere to remember it and every launch started with an empty
// field. This file is the single place we read a key from; the tool configs
// remain the *output*, not the record.
func KeyStorePath() string { return filepath.Join(brewkegHome(), "key") }

// StoreKey records the key. 0600, and the directory 0700: this is a credential
// and it is not covered by the backup/restore machinery (restoring a config
// must not resurrect a revoked key).
func StoreKey(k string) error {
	k = strings.TrimSpace(k)
	if k == "" {
		return nil
	}
	if err := EnsurePrivateDir(brewkegHome()); err != nil {
		return err
	}
	return os.WriteFile(KeyStorePath(), []byte(k), 0o600)
}

// ClearKey forgets the stored key. Called when the user unpools everything.
func ClearKey() error {
	err := os.Remove(KeyStorePath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// StoredKey returns the brewkeg key already configured on this machine, if any.
// The store wins; ~/.claude/settings.json is the fallback so a machine
// configured by an older build, or by hand, still opens with the key present.
func StoredKey() string {
	if k := strings.TrimSpace(ReadFile(KeyStorePath())); k != "" {
		return k
	}
	raw := ReadFile(HomeJoin(".claude", "settings.json"))
	var doc struct {
		Env map[string]string `json:"env"`
	}
	if json.Unmarshal([]byte(raw), &doc) == nil {
		return strings.TrimSpace(doc.Env["ANTHROPIC_AUTH_TOKEN"])
	}
	return ""
}