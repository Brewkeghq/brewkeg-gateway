package brewkeg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// CatalogModel is one row of the gateway's model catalogue. Prices are USD per
// million tokens; the catalogue is generated from the same table the billing
// code reads, so the terminal and the invoice cannot disagree.
type CatalogModel struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Provider      string   `json:"provider"`
	Tier          string   `json:"tier"`
	InputUSD      float64  `json:"inputUsd"`
	OutputUSD     float64  `json:"outputUsd"`
	CacheReadUSD  float64  `json:"cacheReadUsd"`
	CacheWriteUSD float64  `json:"cacheWriteUsd"`
	Discount      float64  `json:"discount"`
	ContextWindow int      `json:"contextWindow"`
	MaxOutput     int      `json:"maxOutput"`
	Capabilities  []string `json:"capabilities"`
}

// Catalog is the served model list.
type Catalog struct {
	Models []CatalogModel `json:"models"`
}

// ByID finds a model row.
func (c Catalog) ByID(id string) (CatalogModel, bool) {
	for _, m := range c.Models {
		if m.ID == id {
			return m, true
		}
	}
	return CatalogModel{}, false
}

// ClaudeFamily returns the model whose id carries the given family token
// (opus, sonnet, haiku, fable). The best match is the shortest id in the
// family, so `claude-opus-5` wins over `claude-opus-5-5`.
func (c Catalog) ClaudeFamily(token string) (CatalogModel, bool) {
	var best CatalogModel
	found := false
	for _, m := range c.Models {
		if !strings.Contains(m.ID, token) {
			continue
		}
		if !found || len(m.ID) < len(best.ID) {
			best = m
			found = true
		}
	}
	return best, found
}

// CatalogEndpoint is where the gateway publishes the catalogue.
func CatalogEndpoint(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/api/catalog"
}

// FetchCatalog pulls the catalogue. A failure is not fatal: the caller falls
// back to the built-in lineup so the CLI still works offline.
func FetchCatalog(ctx context.Context, baseURL string) (Catalog, error) {
	body, err := getJSON(ctx, CatalogEndpoint(baseURL), "")
	if err != nil {
		return Catalog{}, err
	}
	var c Catalog
	if err := json.Unmarshal(body, &c); err != nil {
		return Catalog{}, err
	}
	if len(c.Models) == 0 {
		return Catalog{}, fmt.Errorf("catalogue is empty")
	}
	return c, nil
}

// UsageTotals is one row of usage.
type UsageTotals struct {
	Requests   int     `json:"requests"`
	TokensIn   int     `json:"tokensIn"`
	TokensOut  int     `json:"tokensOut"`
	CacheRead  int     `json:"cacheRead"`
	CacheWrite int     `json:"cacheWrite"`
	SpendUSD   float64 `json:"spendUsd"`
}

// UsageModel is usage broken down by model.
type UsageModel struct {
	Model string `json:"model"`
	UsageTotals
}

// UsageDay is usage broken down by day.
type UsageDay struct {
	Day string `json:"day"`
	UsageTotals
}

// Usage is the caller's own usage for one range.
type Usage struct {
	Range   string       `json:"range"`
	Since   string       `json:"since"`
	Totals  UsageTotals  `json:"totals"`
	ByModel []UsageModel `json:"byModel"`
	Daily   []UsageDay   `json:"daily"`
}

// UsageEndpoint is where the gateway publishes a key's usage.
func UsageEndpoint(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/api/usage"
}

// FetchUsage pulls the usage of the key's owner.
func FetchUsage(ctx context.Context, baseURL, key, window string) (Usage, error) {
	if window == "" {
		window = "7d"
	}
	body, err := getJSON(ctx, UsageEndpoint(baseURL)+"?last="+window, key)
	if err != nil {
		return Usage{}, err
	}
	var u Usage
	if err := json.Unmarshal(body, &u); err != nil {
		return Usage{}, err
	}
	return u, nil
}

// getJSON fetches a URL and returns the body. The key, when present, is sent
// as a bearer token.
func getJSON(ctx context.Context, url, key string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "brewkeg-client/"+Version)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("the gateway rejected this key")
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateway returned %d", res.StatusCode)
	}

	buf := make([]byte, 0, 32*1024)
	tmp := make([]byte, 32*1024)
	for {
		n, err := res.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
		if len(buf) > 4*1024*1024 {
			return nil, fmt.Errorf("response too large")
		}
	}
	return buf, nil
}

// FetchJSON fetches a URL and returns the body.
func FetchJSON(ctx context.Context, url string) ([]byte, error) {
	return getJSON(ctx, url, "")
}

// LatestNpmVersion asks the npm registry for the newest published version of a
// package. Empty when the registry cannot be reached or the package is not on
// npm — an unknown latest version must never be read as "up to date".
func LatestNpmVersion(ctx context.Context, pkg string) string {
	if pkg == "" {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	url := "https://registry.npmjs.org/" + strings.ReplaceAll(pkg, "/", "%2f") + "/latest"
	body, err := getJSON(ctx, url, "")
	if err != nil {
		return ""
	}
	var doc struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(body, &doc) != nil {
		return ""
	}
	return doc.Version
}

// DefaultCatalog is the lineup the CLI falls back to when the gateway cannot
// be reached. Prices are the catalogue's; a stale price here is better than
// no catalogue at all, and the live list wins whenever it is available.
func DefaultCatalog() Catalog {
	rows := []CatalogModel{
		{ID: "claude-opus-5", Name: "claude-opus-5", Provider: "Anthropic", Tier: "max", InputUSD: 3.00, OutputUSD: 15.00, CacheReadUSD: 0.30, CacheWriteUSD: 3.75, ContextWindow: 1_000_000, MaxOutput: 8_192, Capabilities: []string{"text", "streaming", "tool-use"}},
		{ID: "claude-sonnet-5", Name: "claude-sonnet-5", Provider: "Anthropic", Tier: "std", InputUSD: 3.00, OutputUSD: 15.00, CacheReadUSD: 0.30, CacheWriteUSD: 3.75, ContextWindow: 1_000_000, MaxOutput: 8_192, Capabilities: []string{"text", "streaming", "tool-use"}},
		{ID: "claude-haiku-4-5", Name: "claude-haiku-4-5", Provider: "Anthropic", Tier: "eco", InputUSD: 1.00, OutputUSD: 5.00, CacheReadUSD: 0.10, CacheWriteUSD: 1.25, ContextWindow: 200_000, MaxOutput: 8_192, Capabilities: []string{"text", "streaming", "tool-use"}},
	}
	return Catalog{Models: rows}
}
