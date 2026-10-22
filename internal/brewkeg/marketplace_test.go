package brewkeg

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Claude Desktop's Directory → Organization tab is empty until something
// names a marketplace. That something is `allowedPluginMarketplaces` in the
// saved third-party profile — the file this engine already writes.
//
// These tests guard the shape of that entry, because the app is picky: a
// value that is a string instead of an array, or a URL on the wrong origin,
// is silently ignored and the tab stays empty.

func applyDesktop(t *testing.T, o Options) map[string]any {
	t.Helper()
	home(t)
	t.Setenv("BREWKEG_NO_RELAUNCH", "1")
	if o.BaseURL == "" {
		o.BaseURL = "https://brewkeg.dev"
	}
	if o.APIKey == "" {
		o.APIKey = "bk_live_K"
	}
	if _, _, err := ApplyWithSpec(DefaultSpec(), []string{"desktop"}, o); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ClaudeDesktopAppliedConfig())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the written config is not JSON: %v", err)
	}
	return doc
}

func TestDesktopConfigNamesTheMarketplace(t *testing.T) {
	doc := applyDesktop(t, Options{})

	raw, ok := doc["allowedPluginMarketplaces"]
	if !ok {
		t.Fatal("allowedPluginMarketplaces is missing — the Organization tab would stay empty")
	}
	markets, ok := raw.([]any)
	if !ok {
		t.Fatalf("allowedPluginMarketplaces must be a native JSON array, got %T", raw)
	}
	if len(markets) != len(publicMarketplaceRepos)+1 {
		t.Fatalf("expected %d marketplaces, got %d", len(publicMarketplaceRepos)+1, len(markets))
	}
	entry, ok := markets[0].(map[string]any)
	if !ok {
		t.Fatalf("the marketplace entry must be an object, got %T", markets[0])
	}

	if entry["source"] != "url" {
		t.Errorf("source = %v, want url", entry["source"])
	}
	// The credential has to be the inference one: the app only forwards it
	// when the marketplace is on the gateway's own origin.
	if entry["credentialKind"] != "inferenceCredential" {
		t.Errorf("credentialKind = %v, want inferenceCredential", entry["credentialKind"])
	}
	// Entry-level auto_install is clamped to "available" without a manifest
	// digest, so asking for it here would be a lie.
	if entry["installationPreference"] != "available" {
		t.Errorf("installationPreference = %v, want available", entry["installationPreference"])
	}

	url, _ := entry["url"].(string)
	want := "https://brewkeg.dev/api/claude/marketplace.json"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

// The marketplace is only recognised when it sits on the gateway's own
// origin — that is what makes the per-plugin auto-install mark count, and
// what lets the app send the inference credential with it.
func TestMarketplaceURLIsOnTheGatewayOrigin(t *testing.T) {
	for _, base := range []string{"https://brewkeg.dev", "https://brewkeg.dev/", "http://localhost:3000"} {
		doc := applyDesktop(t, Options{BaseURL: base})
		entry := doc["allowedPluginMarketplaces"].([]any)[0].(map[string]any)
		url := entry["url"].(string)
		if strings.Contains(url, "//api") || strings.Contains(url, "///") {
			t.Errorf("base %q produced a broken URL %q", base, url)
		}
		if !strings.HasPrefix(url, strings.TrimRight(base, "/")+"/") {
			t.Errorf("base %q produced %q, which is not on that origin", base, url)
		}
	}
}

// The MCP server is a local process. The app wants an absolute path to an
// executable: a bare name would depend on the PATH the app happened to
// start with, and the app does not start with the user's shell PATH.
func TestDesktopConfigRunsTheMcpServerAsALocalProcess(t *testing.T) {
	doc := applyDesktop(t, Options{})

	raw, ok := doc["managedMcpServers"]
	if !ok {
		t.Fatal("managedMcpServers is missing")
	}
	servers, ok := raw.([]any)
	if !ok {
		t.Fatalf("managedMcpServers must be a native JSON array, got %T", raw)
	}
	entry, ok := servers[0].(map[string]any)
	if !ok {
		t.Fatalf("the server entry must be an object, got %T", servers[0])
	}
	if entry["name"] != "brewkeg" {
		t.Errorf("name = %v, want brewkeg", entry["name"])
	}
	if entry["transport"] != "stdio" {
		t.Errorf("transport = %v, want stdio", entry["transport"])
	}
	command, _ := entry["command"].(string)
	// filepath.IsAbs, not a "/" prefix: on Windows the absolute path is
	// "C:\...\brewkeg.exe", and a Unix-only assertion fails the release
	// on the one platform that has the other path shape.
	if !filepath.IsAbs(command) {
		t.Errorf("command = %q, want an absolute path", command)
	}
	args, ok := entry["args"].([]any)
	if !ok || len(args) != 1 || args[0] != "mcp" {
		t.Errorf("args = %v, want [mcp]", entry["args"])
	}
}

// No credential may be written into a config the app will read: the MCP
// server reads the key the CLI already stored.
func TestDesktopConfigCarriesNoMcpCredential(t *testing.T) {
	doc := applyDesktop(t, Options{APIKey: "bk_live_SECRET"})
	entry := doc["managedMcpServers"].([]any)[0].(map[string]any)
	for _, key := range []string{"headers", "env", "apiKey", "token"} {
		if _, ok := entry[key]; ok {
			t.Errorf("the MCP entry carries %q — the server reads the stored key instead", key)
		}
	}
}

// The marketplace check exists so a setup that leaves the plugin tab empty
// says so, instead of the user finding an empty tab later and blaming it on
// brewkeg.
func TestCheckMarketplaceReportsWhatTheGatewayAnswers(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		served  bool
		message string
	}{
		{
			name:    "served",
			status:  200,
			body:    `{"name":"brewkeg","plugins":[{"name":"brewkeg"}]}`,
			served:  true,
			message: "the brewkeg marketplace is served, with 1 plugin(s)",
		},
		{
			name:    "not deployed yet",
			status:  404,
			message: "the gateway is not serving the plugin marketplace yet",
		},
		{
			name:    "key rejected",
			status:  401,
			message: "the gateway rejected the key while checking the marketplace",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != MarketplacePath {
					t.Errorf("checked %s, want %s", r.URL.Path, MarketplacePath)
				}
				if r.Header.Get("Authorization") != "Bearer bk_live_K" {
					t.Errorf("the check must send the key the way the app does, got %q", r.Header.Get("Authorization"))
				}
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()

			got := CheckMarketplace(context.Background(), srv.URL, "bk_live_K")
			if got.Served != c.served {
				t.Errorf("served = %v, want %v (%s)", got.Served, c.served, got.Message)
			}
			if got.Message != c.message {
				t.Errorf("message = %q, want %q", got.Message, c.message)
			}
		})
	}
}

// The marketplace must be reachable with the same bearer the app sends, so
// the check cannot pass on a path the app would fail on.
func TestMarketplacePathIsTheOneTheConfigNames(t *testing.T) {
	if MarketplacePath != "/api/claude/marketplace.json" {
		t.Errorf("MarketplacePath = %q", MarketplacePath)
	}
}

// The catalogue is not ours. Anthropic publishes it, the app clones it, and
// naming it here is what puts Figma, Miro, Canva, Notion, Linear, Slack,
// Sentry and the rest in the Directory → Organization tab with no second
// command and nothing re-hosted by us.
//
// The first five are the ones Claude Desktop ships in its own browse list, so
// naming them adds nothing that Anthropic has not already decided to offer.
// The rest are publishers shipping their own catalogue: the app would show
// them to anyone who went looking, and this is how a brewkeg user gets them
// without going looking.
func TestDesktopConfigNamesThePublicPluginCatalogues(t *testing.T) {
	doc := applyDesktop(t, Options{})
	markets := doc["allowedPluginMarketplaces"].([]any)

	want := []string{
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
	if len(want) != len(publicMarketplaceRepos) {
		t.Fatalf("the test names %d repositories, the catalogue has %d", len(want), len(publicMarketplaceRepos))
	}

	seen := 0
	for _, m := range markets[1:] {
		entry := m.(map[string]any)
		if entry["source"] != "github" {
			continue
		}
		repo, _ := entry["repo"].(string)
		known := false
		for _, w := range want {
			if w == repo {
				known = true
				break
			}
		}
		if !known {
			t.Errorf("unexpected marketplace %q", repo)
			continue
		}
		seen++
		// A public repo needs no token, and a token in a config file is a
		// credential in a place the user may share.
		if entry["credentialKind"] != "anonymous" {
			t.Errorf("%s: credentialKind = %v, want anonymous", repo, entry["credentialKind"])
		}
		// Entry-level auto_install is clamped to "available" without a 40-char
		// commit SHA, so asking for it would be a lie.
		if entry["installationPreference"] != "available" {
			t.Errorf("%s: installationPreference = %v, want available", repo, entry["installationPreference"])
		}
		// No ref and no expectedName: the app clones the default branch, and
		// a publisher renaming their own catalogue should not cost the user
		// their plugins.
		if _, ok := entry["ref"]; ok {
			t.Errorf("%s: a pinned ref would break when the publisher renames the branch", repo)
		}
		if _, ok := entry["expectedName"]; ok {
			t.Errorf("%s: a pinned expectedName would reject a renamed marketplace", repo)
		}
	}
	if seen != len(want) {
		t.Errorf("named %d of the %d public marketplaces", seen, len(want))
	}
}

// Every vendor connector we could name as a managed server already ships
// inside its own plugin, under the same name. A managed entry with that name
// makes the app drop the plugin's server, so the connector would arrive
// without the skills that explain it.
func TestDesktopConfigNamesNoThirdPartyMcpServer(t *testing.T) {
	doc := applyDesktop(t, Options{})
	servers := doc["managedMcpServers"].([]any)
	if len(servers) != 1 {
		t.Fatalf("expected one managed server, got %d", len(servers))
	}
	for _, m := range servers {
		entry := m.(map[string]any)
		if entry["name"] != "brewkeg" {
			t.Errorf("managed MCP server %v — third-party servers belong in their plugin", entry["name"])
		}
	}
}

// The setup prints how many marketplaces it named. It has to be the number
// that was written, not a number somebody typed into the message.
func TestMarketplaceCountReadsTheSpecItWillWrite(t *testing.T) {
	s := DefaultSpec()
	if got := MarketplaceCount(s); got != len(publicMarketplaceRepos)+1 {
		t.Errorf("MarketplaceCount = %d, want %d", got, len(publicMarketplaceRepos)+1)
	}
	if got := MarketplaceCount(Spec{}); got != 0 {
		t.Errorf("MarketplaceCount of an empty spec = %d, want 0", got)
	}
}

// Settings → Import is closed by default on a third-party deployment: the
// page says "Import isn't enabled for this deployment. Contact your
// organization's administrator to turn it on." brewkeg is the deployment, so
// the setup says yes.
//
// The value has to land as a JSON object with a boolean in it. A quoted
// "true" is a string, and the app does not read it as the switch.
func TestDesktopConfigTurnsTheImportPageOn(t *testing.T) {
	doc := applyDesktop(t, Options{})
	raw, ok := doc["claudeAiImport"]
	if !ok {
		t.Fatal("claudeAiImport is missing — the Import page stays closed")
	}
	block, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("claudeAiImport must be an object, got %T", raw)
	}
	if block["enabled"] != true {
		t.Errorf("claudeAiImport.enabled = %v, want true", block["enabled"])
	}
	// Export is a second switch. Without it the app brings conversations in
	// and never hands them back.
	if block["exportEnabled"] != true {
		t.Errorf("claudeAiImport.exportEnabled = %v, want true", block["exportEnabled"])
	}
}
