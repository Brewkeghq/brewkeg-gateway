// Copyright (c) 2026 brewkeg. All rights reserved.
// See LICENSE — this software is proprietary and not licensed for reuse.
package brewkeg

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Developer mode exists for one job: pointing the app at a gateway that is not
// brewkeg.dev — a local build, a staging deploy, a colleague's box. It changes
// the URL the app talks to AND the URL it writes into the user's tool configs,
// so it is off by default and stored apart from the settings a normal user
// touches.
//
// The override lives in its own file rather than in config.json. config.json is
// the user's no-clicks setup; a file that silently repoints every tool at a
// localhost gateway belongs somewhere a developer can find and delete it.
const devFile = "developer.json"

// DevSettings is what a developer may override, and nothing else. There is
// deliberately no key here: the key is a credential, and a file that exists to
// be shared with a colleague must not be worth sharing.
type DevSettings struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"baseUrl,omitempty"`
}

// DevSettingsPath is ~/.brewkeg/developer.json.
func DevSettingsPath() string { return filepath.Join(brewkegHome(), devFile) }

// ReadDevSettings returns the stored overrides, or the zero value. A missing or
// unreadable file is not an error: developer mode is opt-in, and a corrupt file
// must not stop the app from opening.
func ReadDevSettings() DevSettings {
	var s DevSettings
	b, err := os.ReadFile(DevSettingsPath())
	if err != nil {
		return DevSettings{}
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return DevSettings{}
	}
	s.BaseURL = strings.TrimSpace(s.BaseURL)
	if s.Enabled && s.BaseURL == "" {
		// Enabled with nothing to point at is just the default URL with extra
		// steps; treat it as off so the UI does not claim a custom gateway.
		s.Enabled = false
	}
	return s
}

// DevBaseURL is the override in force, or "" when developer mode is off. It is
// validated on read as well as on write, because the file is hand-editable and
// a typo here becomes a broken config in every tool on the machine.
func DevBaseURL() string {
	s := ReadDevSettings()
	if !s.Enabled {
		return ""
	}
	if !validBaseURL(s.BaseURL) {
		return ""
	}
	return strings.TrimRight(s.BaseURL, "/")
}

// DevModeOn reports whether developer mode is enabled, whether or not it has a
// usable URL.
func DevModeOn() bool { return ReadDevSettings().Enabled }

// SetDevBaseURL validates, stores, and returns an error describing exactly what
// is wrong. The caller shows it verbatim: the alternative is a settings screen
// that accepts "localhost:3000" and silently writes a malformed base URL into
// four tools.
func SetDevBaseURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		// Clearing the URL turns developer mode off rather than leaving it on
		// and inert.
		return SaveDevSettings(DevSettings{})
	}
	if err := validateBaseURL(raw); err != nil {
		return err
	}
	return SaveDevSettings(DevSettings{Enabled: true, BaseURL: strings.TrimRight(raw, "/")})
}

// SetDevMode turns the override on or off. Turning it off does NOT clear the
// typed URL: a developer toggling it off to check what production looks like
// will want it back on in a second, and re-typing a localhost URL to find that
// out is the kind of friction that gets a bug missed.
func SetDevMode(on bool) error {
	s := ReadDevSettings()
	if !on {
		return SaveDevSettings(DevSettings{BaseURL: s.BaseURL})
	}
	if s.BaseURL == "" {
		return fmt.Errorf("set a gateway URL first")
	}
	if err := validateBaseURL(s.BaseURL); err != nil {
		return err
	}
	return SaveDevSettings(DevSettings{Enabled: true, BaseURL: strings.TrimRight(s.BaseURL, "/")})
}

// ClearDevSettings removes the file outright. Toggling off would leave a
// developer.json on disk holding the URL of an internal host, which is not
// something to leave behind on a shared machine.
func ClearDevSettings() error {
	err := os.Remove(DevSettingsPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// SaveDevSettings writes the file owner-only. It sits next to the key store and
// is often edited by hand, so a world-readable copy of an internal hostname is
// not something to leave lying around.
//
// Enabled=false with a URL is a legitimate state — the toggle remembers what was
// typed — so this is not the place to collapse it to off.
func SaveDevSettings(s DevSettings) error {
	s.BaseURL = strings.TrimSpace(s.BaseURL)
	if s.BaseURL != "" {
		if err := validateBaseURL(s.BaseURL); err != nil {
			return err
		}
		s.BaseURL = strings.TrimRight(s.BaseURL, "/")
	}
	if err := os.MkdirAll(brewkegHome(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	// 0600, not WriteFile's 0644: this names an internal host and sits in the
	// same directory as the API key, and umask will not save you on a machine
	// whose default is 000.
	if err := os.WriteFile(DevSettingsPath(), b, 0o600); err != nil {
		return err
	}
	return os.Chmod(DevSettingsPath(), 0o600)
}

func validBaseURL(raw string) bool { return validateBaseURL(raw) == nil }

// validateBaseURL insists on a scheme and a host. The bare "localhost:3000"
// reads as a scheme of "localhost" to url.Parse and would be written into
// ~/.codex/config.toml as a base_url no client can resolve.
func validateBaseURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("enter a gateway URL")
	}
	if !strings.Contains(raw, "://") {
		return fmt.Errorf("include the scheme — try http://%s", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("that is not a URL: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("use http:// or https://, not %s://", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("no host in %q", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("the URL must not carry a query string or fragment")
	}
	return nil
}
