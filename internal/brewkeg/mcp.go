package brewkeg

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// The brewkeg MCP server.
//
// Claude Desktop and Claude Code can ask brewkeg things through an MCP
// server. This one is a local process: the app starts it, it reads the key
// the CLI already stored, and every answer comes from the gateway.
//
// Nothing is hardcoded here. A price in an answer is the price on the
// invoice, because the answer IS the catalogue; a usage figure is the
// caller's own rows. That is the whole point of serving the catalogue over
// MCP instead of shipping a copy of it in a skill.
//
// Transport: MCP over stdio. One JSON-RPC message per line. stdout carries
// protocol messages and nothing else — a stray print there is a corrupt
// stream — so every diagnostic goes to stderr.

// MCPRequest is one JSON-RPC message from the client.
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// MCPResponse is one JSON-RPC message to the client.
type MCPResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *MCPError       `json:"error,omitempty"`
}

// MCPError is a JSON-RPC error object.
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPTool is one tool the server offers.
type MCPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// MCPServer answers MCP requests over stdio.
type MCPServer struct {
	// BaseURL is the gateway. Every answer comes from here.
	BaseURL string
	// APIKey identifies the caller. Empty means the tools that need an
	// account say so instead of guessing.
	APIKey string
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
}

// Serve reads requests until the input ends. A line that is not a JSON-RPC
// message is skipped, not fatal: one bad message should not take the server
// down mid-session.
func (s *MCPServer) Serve(ctx context.Context) error {
	sc := bufio.NewScanner(s.In)
	// A tool result can be long, and the default 64KB line limit would cut
	// a catalogue listing in half.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	enc := json.NewEncoder(s.Out)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req MCPRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.logf("ignoring an unreadable message: %v", err)
			continue
		}
		resp, reply := s.dispatch(ctx, req)
		if !reply {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *MCPServer) logf(format string, args ...any) {
	if s.Err == nil {
		return
	}
	fmt.Fprintf(s.Err, "brewkeg mcp: "+format+"\n", args...)
}

func (s *MCPServer) ok(id json.RawMessage, result any) MCPResponse {
	return MCPResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func (s *MCPServer) fail(id json.RawMessage, code int, message string) MCPResponse {
	return MCPResponse{JSONRPC: "2.0", ID: id, Error: &MCPError{Code: code, Message: message}}
}

// dispatch handles one message. The second return says whether to reply: a
// notification (no id) gets no answer.
func (s *MCPServer) dispatch(ctx context.Context, req MCPRequest) (MCPResponse, bool) {
	notification := len(req.ID) == 0

	switch req.Method {
	case "initialize":
		return s.ok(req.ID, s.initialize(req.Params)), !notification
	case "tools/list":
		return s.ok(req.ID, map[string]any{"tools": s.tools()}), !notification
	case "tools/call":
		return s.ok(req.ID, s.call(ctx, req.Params)), !notification
	case "ping":
		return s.ok(req.ID, map[string]any{}), !notification
	case "resources/list":
		return s.ok(req.ID, map[string]any{"resources": []any{}}), !notification
	case "prompts/list":
		return s.ok(req.ID, map[string]any{"prompts": []any{}}), !notification
	case "notifications/initialized", "notifications/cancelled":
		return MCPResponse{}, false
	default:
		if notification {
			return MCPResponse{}, false
		}
		return s.fail(req.ID, -32601, "unknown method "+req.Method), true
	}
}

// initialize answers the handshake. The client's protocol version is echoed
// back when we can speak it, which is the rule the spec sets.
func (s *MCPServer) initialize(params json.RawMessage) map[string]any {
	version := "2024-11-05"
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if json.Unmarshal(params, &p) == nil && p.ProtocolVersion != "" {
		version = p.ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": "brewkeg", "version": Version},
	}
}

func (s *MCPServer) tools() []MCPTool {
	return []MCPTool{
		{
			Name: "brewkeg_models",
			Description: "The models brewkeg serves, with their price, context window and capabilities. " +
				"Prices are USD per million tokens and come from the live catalogue. " +
				"Use this before recommending a model: the ids and prices here are the ones that work.",
			InputSchema: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			Name: "brewkeg_usage",
			Description: "The caller's own brewkeg usage and estimated cost for a range. " +
				"The cost is an estimate at published rates, not an invoice.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"range": map[string]any{
						"type":        "string",
						"enum":        []string{"24h", "7d", "30d", "90d"},
						"description": "How far back to look. Defaults to 7d.",
					},
				},
				"additionalProperties": false,
			},
		},
		{
			Name: "brewkeg_plans",
			Description: "The brewkeg plans on offer, with their monthly price, token quota and request quota. " +
				"Top-up credit does not expire.",
			InputSchema: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
	}
}

// call runs one tool and wraps the answer as MCP content.
func (s *MCPServer) call(ctx context.Context, params json.RawMessage) map[string]any {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return toolError("that request could not be read")
	}

	text, err := s.run(ctx, p.Name, p.Arguments)
	if err != nil {
		return toolError(err.Error())
	}
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
	}
}

// toolError is a failed tool call. The text is still content: the model
// should read what went wrong and say so, rather than the call vanishing.
func toolError(message string) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": message}},
		"isError": true,
	}
}

func (s *MCPServer) run(ctx context.Context, name string, args map[string]any) (string, error) {
	switch name {
	case "brewkeg_models":
		return s.models(ctx)
	case "brewkeg_usage":
		return s.usage(ctx, args)
	case "brewkeg_plans":
		return s.plans(ctx)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

// models lists the live catalogue.
func (s *MCPServer) models(ctx context.Context) (string, error) {
	cat, err := FetchCatalog(ctx, s.BaseURL)
	if err != nil {
		return "", fmt.Errorf("could not reach the brewkeg catalogue: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "brewkeg serves %d models. Prices are USD per million tokens.\n\n", len(cat.Models))
	b.WriteString("| id | tier | input | output | cache read | context | notes |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, m := range cat.Models {
		notes := []string{}
		if m.Discount > 0 {
			notes = append(notes, fmt.Sprintf("%.0f%% under list", m.Discount))
		}
		for _, c := range m.Capabilities {
			if c == "reasoning" {
				notes = append(notes, "reasoning")
			}
		}
		fmt.Fprintf(&b, "| %s | %s | %.2f | %.2f | %.2f | %s | %s |\n",
			m.ID, m.Tier, m.InputUSD, m.OutputUSD, m.CacheReadUSD,
			humanTokens(m.ContextWindow), strings.Join(notes, ", "))
	}
	return b.String(), nil
}

// usage reports the caller's own usage.
func (s *MCPServer) usage(ctx context.Context, args map[string]any) (string, error) {
	if s.APIKey == "" {
		return "", fmt.Errorf("no brewkeg key on this machine — run `brewkeg login` first")
	}
	window := "7d"
	if v, ok := args["range"].(string); ok && v != "" {
		window = v
	}
	u, err := FetchUsage(ctx, s.BaseURL, s.APIKey, window)
	if err != nil {
		return "", fmt.Errorf("could not read usage: %w", err)
	}

	t := u.Totals
	var b strings.Builder
	fmt.Fprintf(&b, "brewkeg usage over %s (since %s)\n", u.Range, u.Since)
	fmt.Fprintf(&b, "%s requests · %s in · %s out · %s cache read · %s cache write · about %s\n",
		thousands(t.Requests), humanTokens(t.TokensIn), humanTokens(t.TokensOut),
		humanTokens(t.CacheRead), humanTokens(t.CacheWrite), money(t.SpendUSD))
	if len(u.ByModel) > 0 {
		b.WriteString("\nby model:\n")
		for _, m := range u.ByModel {
			fmt.Fprintf(&b, "- %s: %s requests, %s in, %s out, about %s\n",
				m.Model, thousands(m.Requests), humanTokens(m.TokensIn), humanTokens(m.TokensOut), money(m.SpendUSD))
		}
	}
	b.WriteString("\nThe cost is an estimate at brewkeg's published rates, not an invoice.")
	return b.String(), nil
}

// plans lists the plans on offer.
func (s *MCPServer) plans(ctx context.Context) (string, error) {
	body, err := FetchJSON(ctx, strings.TrimRight(s.BaseURL, "/")+"/api/plans")
	if err != nil {
		return "", fmt.Errorf("could not reach the plans: %w", err)
	}
	var doc struct {
		Plans []struct {
			Name                string `json:"name"`
			MonthlyPriceKobo    int    `json:"monthlyPriceKobo"`
			MonthlyPriceCents   int    `json:"monthlyPriceCents"`
			MonthlyTokenQuota   *int   `json:"monthlyTokenQuota"`
			MonthlyRequestQuota *int   `json:"monthlyRequestQuota"`
		} `json:"plans"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("the plans could not be read: %w", err)
	}
	if len(doc.Plans) == 0 {
		return "brewkeg has no plans on offer right now.", nil
	}

	var b strings.Builder
	b.WriteString("brewkeg plans. Top-up credit does not expire.\n\n")
	b.WriteString("| plan | price | tokens | requests |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, p := range doc.Plans {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n",
			p.Name, planPrice(p.MonthlyPriceCents, p.MonthlyPriceKobo),
			quota(p.MonthlyTokenQuota), quota(p.MonthlyRequestQuota))
	}
	return b.String(), nil
}

// planPrice shows the price in the currency the plan is sold in. A plan with
// no USD price is NGN-only, so the kobo price is the one that is real.
func planPrice(cents, kobo int) string {
	if cents > 0 {
		return fmt.Sprintf("$%.2f", float64(cents)/100)
	}
	return fmt.Sprintf("₦%.0f", float64(kobo)/100)
}

func quota(v *int) string {
	if v == nil {
		return "no limit"
	}
	return humanTokens(*v)
}

func thousands(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

// humanTokens is a token count in the shortest honest unit: 1.2M, 300K, 512.
func humanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dK", n/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func money(v float64) string {
	return fmt.Sprintf("$%.2f", v)
}
