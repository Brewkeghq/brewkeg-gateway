package brewkeg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// The plugin marketplace brewkeg publishes.
//
// Claude Desktop's plugin tab is empty until something names a marketplace.
// brewkeg names one on the gateway's own origin: the app fetches the manifest
// with the same bearer it sends for inference, downloads the plugin archive,
// checks its digest, and installs the plugin — on its own, with no click in
// the app.
//
// This file is the CLI's side of that. The path has to match the one the
// desktop profile points at, and the gateway has to actually be serving it. A
// setup that silently leaves the tab empty is the failure this exists to
// prevent: the config is right, the feature is not, and nothing says so.

// MarketplacePath is where the marketplace manifest is served. It is also
// what the desktop profile names, so the two cannot drift.
const MarketplacePath = "/api/claude/marketplace.json"

// publicMarketplaceRepos are the public plugin marketplaces Claude Desktop
// can install from.
//
// The app clones each repo itself and reads the same public marketplace.json
// everyone else reads, so naming them here costs the user nothing and costs
// us nothing: no bytes re-hosted, no digest of ours attached to their code.
// It also needs git on the machine, which setup warns about.
//
// The list must match web2/lib/claude-marketplaces.ts. The gateway's spec
// wins at runtime — this is the offline fallback, and a spec written for an
// older release still does the useful part.
//
// Verified 2026-10-22: each repo was public, had a .claude-plugin/
// marketplace.json on its default branch, and declared a unique name.
var publicMarketplaceRepos = []string{
	"anthropics/knowledge-work-plugins",
	"anthropics/claude-plugins-official",
	"anthropics/financial-services",
	"anthropics/claude-for-legal",
	"anthropics/life-sciences",
	"adobe/skills",
	"grafana/ai-marketplace",
	"miroapp/miro-ai",
	"canva-sdks/canva-skills",
	"stripe/ai",
	"getsentry/plugin-claude",
	"vercel/vercel-plugin",
	"exa-labs/exa-mcp-server",
	"tavily-ai/skills",
	"box/skills",
	"planetscale/claude-plugin",
	"huggingface/skills",
	"resend/resend-skills",
}

// MarketplaceCount is how many marketplaces the desktop profile names, ours
// included. It is read from the spec that was written, so the number setup
// prints is the number on the machine.
func MarketplaceCount(s Spec) int {
	for _, t := range s.Targets {
		if t.ID != "desktop" {
			continue
		}
		for _, f := range t.Files {
			if f.Kind != "desktop-3p" {
				continue
			}
			for _, e := range f.Entries {
				if e.Name != "allowedPluginMarketplaces" {
					continue
				}
				var entries []map[string]any
				if err := json.Unmarshal([]byte(e.Value), &entries); err != nil {
					return 0
				}
				return len(entries)
			}
		}
	}
	return 0
}

// desktopMarketplacesValue is the allowedPluginMarketplaces array.
//
// Ours first: a url entry, because the marketplace is a file we serve rather
// than a git repo, with the inference credential, because the manifest is on
// the gateway's origin. Then the public catalogues, one per repo.
//
// No ref and no expectedName on the github entries. The app clones the
// default branch, and a publisher renaming their own catalogue should not
// cost the user their plugins. The repo slug is the only thing asserted.
func desktopMarketplacesValue() string {
	entries := []string{
		`{"source":"url","url":"{{origin}}` + MarketplacePath + `","credentialKind":"inferenceCredential","installationPreference":"available"}`,
	}
	for _, repo := range publicMarketplaceRepos {
		entries = append(entries, fmt.Sprintf(
			`{"source":"github","repo":%q,"credentialKind":"anonymous","installationPreference":"available"}`,
			repo,
		))
	}
	return "[" + strings.Join(entries, ",") + "]"
}

// desktopManagedMcpServersValue is the managedMcpServers array.
//
// One server: ours, as a local process. The app starts it and the binary
// reads the key it already stored, so no credential is written into any
// config.
//
// Deliberately no third-party server. Every vendor connector we could name
// here already ships inside its own plugin — Figma's plugin bundles Figma's
// MCP server — and a managed entry with the same name makes the app drop the
// plugin's server in favour of ours. So the connector arrives with the skills
// that explain it, instead of as a bare server to discover.
func desktopManagedMcpServersValue() string {
	return `[{"name":"brewkeg","transport":"stdio","command":"{{selfPath}}","args":["mcp"]}]`
}

// MarketplaceStatus is what a check of the marketplace found.
type MarketplaceStatus struct {
	// Served means the gateway answered with a marketplace manifest.
	Served bool
	// Name is the marketplace's name, when it was served.
	Name string
	// Plugins is how many plugins the manifest lists.
	Plugins int
	// Message is one line for a person.
	Message string
}

// CheckMarketplace asks the gateway for the marketplace manifest.
//
// The key is sent as a bearer because that is how the app fetches it, so this
// checks the same path the app will take — not a public one that would answer
// while the app's does not.
func CheckMarketplace(ctx context.Context, baseURL, key string) MarketplaceStatus {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := strings.TrimRight(baseURL, "/") + MarketplacePath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return MarketplaceStatus{Message: "the marketplace address is not a valid URL"}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "brewkeg-client/"+Version)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return MarketplaceStatus{Message: "could not reach the gateway to check the marketplace"}
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return MarketplaceStatus{Message: "the gateway rejected the key while checking the marketplace"}
	case http.StatusNotFound:
		return MarketplaceStatus{Message: "the gateway is not serving the plugin marketplace yet"}
	default:
		return MarketplaceStatus{Message: fmt.Sprintf("the gateway answered %d while checking the marketplace", res.StatusCode)}
	}

	var manifest struct {
		Name    string `json:"name"`
		Plugins []any  `json:"plugins"`
	}
	if err := json.NewDecoder(res.Body).Decode(&manifest); err != nil {
		return MarketplaceStatus{Message: "the marketplace answer could not be read"}
	}
	if manifest.Name == "" {
		return MarketplaceStatus{Message: "the marketplace answer has no name"}
	}
	return MarketplaceStatus{
		Served:  true,
		Name:    manifest.Name,
		Plugins: len(manifest.Plugins),
		Message: fmt.Sprintf("the %s marketplace is served, with %d plugin(s)", manifest.Name, len(manifest.Plugins)),
	}
}
