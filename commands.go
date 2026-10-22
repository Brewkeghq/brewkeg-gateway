package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/brewkeghq/brewkeg-gateway/internal/brewkeg"
)

// runModels prints the catalogue the gateway serves, with prices.
func runModels(args []string) int {
	cat, live := loadCatalog()
	if !live {
		fmt.Println("(showing the built-in list — the gateway could not be reached)")
	}
	fmt.Printf("%-30s %-9s %-9s %-9s %s\n", "MODEL", "CONTEXT", "$/M IN", "$/M OUT", "AGENTS")
	for _, m := range cat.Models {
		fmt.Printf("%-30s %-9s %-9s %-9s %s\n",
			m.ID,
			contextLabel(m.ContextWindow),
			priceLabel(m.InputUSD),
			priceLabel(m.OutputUSD),
			agentLabel(m.ID),
		)
	}
	return 0
}

// loadCatalog fetches the live catalogue, falling back to the built-in list.
func loadCatalog() (brewkeg.Catalog, bool) {
	cat, err := brewkeg.FetchCatalog(context.Background(), brewkeg.UserBaseURL())
	if err != nil {
		return brewkeg.DefaultCatalog(), false
	}
	return cat, true
}

func contextLabel(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%dM", n/1_000_000)
	}
	return fmt.Sprintf("%dK", n/1000)
}

func priceLabel(v float64) string {
	if v == 0 {
		return "FREE"
	}
	return fmt.Sprintf("%.2f", v)
}

// agentLabel is the agent a model is tuned for.
func agentLabel(id string) string {
	switch {
	case strings.HasPrefix(id, "claude-"):
		return "claude"
	case strings.HasPrefix(id, "gpt-"):
		return "codex"
	}
	return "opencode,pi,kilo,command-code"
}

// resolveKey finds the key to act with: the environment first, then the stored
// key, then the config file. One order for every command, so the CLI and the
// desktop app never act as two different users.
func resolveKey() string {
	if k := strings.TrimSpace(os.Getenv("BREWKEG_API_KEY")); k != "" {
		return k
	}
	if k := brewkeg.StoredKey(); k != "" {
		return k
	}
	return brewkeg.LoadUserConfig().APIKey
}

// runUsage prints the caller's own usage for a range.
func runUsage(args []string) int {
	range_ := flagValue(args, "--last")
	if range_ == "" {
		range_ = "7d"
	}
	asJSON := hasFlag(args, "--json")

	key := resolveKey()
	if key == "" {
		fmt.Fprintln(os.Stderr, "brewkeg: no API key — run `brewkeg login` first")
		return 1
	}

	u, err := brewkeg.FetchUsage(context.Background(), brewkeg.UserBaseURL(), key, range_)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brewkeg: %v\n", err)
		return 1
	}

	if asJSON {
		raw, _ := json.MarshalIndent(u, "", "  ")
		fmt.Println(string(raw))
		return 0
	}

	t := u.Totals
	fmt.Printf("brewkeg usage · last %s · %d requests · input %d · output %d · cache read %d · cache write %d\n",
		u.Range, t.Requests, t.TokensIn, t.TokensOut, t.CacheRead, t.CacheWrite)
	if len(u.ByModel) > 0 {
		fmt.Println()
		fmt.Printf("%-26s %9s %12s %12s %12s %10s\n", "MODEL", "REQUESTS", "INPUT", "OUTPUT", "CACHE READ", "COST")
		for _, m := range u.ByModel {
			fmt.Printf("%-26s %9d %12d %12d %12d %10s\n",
				m.Model, m.Requests, m.TokensIn, m.TokensOut, m.CacheRead, fmt.Sprintf("$%.4f", m.SpendUSD))
		}
	}
	fmt.Println("\nCost is an estimate at brewkeg's published rates, not an invoice.")
	return 0
}

// runLogin stores the API key.
func runLogin(args []string) int {
	key := flagValue(args, "--api-key")
	if key == "" {
		key = strings.TrimSpace(os.Getenv("BREWKEG_API_KEY"))
	}
	if key == "" {
		key = promptSecret("Paste your brewkeg API key (from brewkeg.dev/dashboard/keys)")
	}
	if key == "" {
		fmt.Fprintln(os.Stderr, "brewkeg: no key given — nothing changed")
		return 1
	}

	fmt.Println("\n  Checking the key…")
	res := checkKey(context.Background(), brewkeg.UserBaseURL(), key)
	fmt.Printf("  %s\n", res.Message)
	if res.Rejected {
		fmt.Fprintln(os.Stderr, "\n  Nothing was saved. Check the key and try again.")
		return 1
	}

	if err := brewkeg.StoreKey(key); err != nil {
		fmt.Fprintf(os.Stderr, "brewkeg: %v\n", err)
		return 1
	}

	cfg := brewkeg.LoadUserConfig()
	cfg.APIKey = key
	if err := brewkeg.SaveUserConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "brewkeg: %v\n", err)
		return 1
	}

	fmt.Printf("\n  ✔ Key saved to %s (%s)\n", brewkeg.KeyStorePath(), brewkeg.MaskKey(key))
	fmt.Println("  Next:  brewkeg claude        (run Claude Code on brewkeg)")
	return 0
}

// runMCP serves the brewkeg MCP server over stdio.
//
// Claude Desktop and Claude Code start this themselves; it is not something a
// person runs by hand. stdout carries protocol messages and nothing else, so
// every diagnostic goes to stderr.
func runMCP(args []string) int {
	base := brewkeg.UserBaseURL()
	if v := flagValue(args, "--base-url"); v != "" {
		base = strings.TrimRight(v, "/")
	}
	key := resolveKey()

	srv := &brewkeg.MCPServer{
		BaseURL: base,
		APIKey:  key,
		In:      os.Stdin,
		Out:     os.Stdout,
		Err:     os.Stderr,
	}
	if err := srv.Serve(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "brewkeg mcp: %v\n", err)
		return 1
	}
	return 0
}

// keyCheck is the result of asking the gateway about a key.
type keyCheck struct {
	Reachable bool
	Accepted  bool
	Rejected  bool
	Message   string
}

// checkKey asks the gateway whether a key works. A key the gateway refuses is
// reported as rejected; a gateway we could not reach is never reported as a
// bad key.
func checkKey(ctx context.Context, baseURL, key string) keyCheck {
	_, err := brewkeg.FetchUsage(ctx, baseURL, key, "24h")
	switch {
	case err == nil:
		return keyCheck{Reachable: true, Accepted: true, Message: "the gateway accepted this key"}
	case strings.Contains(err.Error(), "rejected this key"):
		return keyCheck{Reachable: true, Rejected: true, Message: "the gateway rejected this key"}
	default:
		return keyCheck{Message: err.Error()}
	}
}

// latestNpmVersion asks the npm registry for the newest published version.
func latestNpmVersion(pkg string) string {
	return brewkeg.LatestNpmVersion(context.Background(), pkg)
}
