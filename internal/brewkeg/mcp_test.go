package brewkeg

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The MCP server is what makes brewkeg answerable inside a coding session.
// It speaks MCP over stdio: one JSON-RPC message per line.
//
// The rules it has to keep:
//   - a notification (no id) gets no reply;
//   - the tool answers come from the gateway, never from a copy of the
//     catalogue that can go stale;
//   - a tool that needs an account says so instead of guessing.

func mcpGateway(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/catalog":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []any{
					map[string]any{
						"id": "claude-opus-5", "name": "Opus 5", "provider": "anthropic",
						"tier": "max", "inputUsd": 3.0, "outputUsd": 15.0, "cacheReadUsd": 0.3,
						"discount": 40, "contextWindow": 1000000, "maxOutput": 64000,
						"capabilities": []string{"text", "streaming", "reasoning"},
					},
					map[string]any{
						"id": "claude-haiku-4-5", "name": "Haiku 4.5", "provider": "anthropic",
						"tier": "eco", "inputUsd": 0.5, "outputUsd": 2.5, "cacheReadUsd": 0.05,
						"discount": 0, "contextWindow": 200000, "maxOutput": 32000,
						"capabilities": []string{"text"},
					},
				},
			})
		case "/api/usage":
			if r.Header.Get("Authorization") != "Bearer bk_live_K" {
				w.WriteHeader(401)
				return
			}
			if got := r.URL.Query().Get("last"); got != "30d" {
				t.Errorf("usage range = %q, want 30d", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"range": "30d", "since": "2026-09-22T00:00:00Z",
				"totals": map[string]any{
					"requests": 1234, "tokensIn": 2300000, "tokensOut": 456000,
					"cacheRead": 1200000, "cacheWrite": 300000, "spendUsd": 12.5,
				},
				"byModel": []any{
					map[string]any{
						"model": "claude-opus-5", "requests": 800, "tokensIn": 1500000,
						"tokensOut": 300000, "spendUsd": 9.0,
					},
				},
			})
		case "/api/plans":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"plans": []any{
					map[string]any{
						"name": "PAYG", "monthlyPriceKobo": 500000, "monthlyPriceCents": 1000,
						"monthlyTokenQuota": 50000000, "monthlyRequestQuota": 1000,
					},
				},
			})
		default:
			w.WriteHeader(404)
		}
	}))
}

// ask sends one request and returns the reply.
func ask(t *testing.T, srv *MCPServer, request string) map[string]any {
	t.Helper()
	in := strings.NewReader(request + "\n")
	var out bytes.Buffer
	srv.In = in
	srv.Out = &out
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(out.String())
	if line == "" {
		return nil
	}
	var reply map[string]any
	if err := json.Unmarshal([]byte(line), &reply); err != nil {
		t.Fatalf("reply is not JSON: %v (%q)", err, line)
	}
	return reply
}

func TestMCPInitializeAnswersTheHandshake(t *testing.T) {
	srv := &MCPServer{BaseURL: "https://brewkeg.dev"}
	reply := ask(t, srv, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)

	result, ok := reply["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", reply)
	}
	if result["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v, want the client's own", result["protocolVersion"])
	}
	caps, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("no capabilities in %v", result)
	}
	if _, ok := caps["tools"]; !ok {
		t.Error("the server must advertise the tools capability")
	}
}

func TestMCPListTools(t *testing.T) {
	srv := &MCPServer{BaseURL: "https://brewkeg.dev"}
	reply := ask(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)

	result := reply["result"].(map[string]any)
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("no tools in %v", reply)
	}
	names := map[string]bool{}
	for _, tool := range tools {
		tl := tool.(map[string]any)
		names[tl["name"].(string)] = true
		if tl["description"] == "" {
			t.Errorf("tool %v has no description", tl["name"])
		}
		if _, ok := tl["inputSchema"]; !ok {
			t.Errorf("tool %v has no input schema", tl["name"])
		}
	}
	for _, want := range []string{"brewkeg_models", "brewkeg_usage", "brewkeg_plans"} {
		if !names[want] {
			t.Errorf("tool %q is missing", want)
		}
	}
}

func TestMCPModelsToolReadsTheGateway(t *testing.T) {
	gw := mcpGateway(t)
	defer gw.Close()

	srv := &MCPServer{BaseURL: gw.URL}
	reply := ask(t, srv, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"brewkeg_models","arguments":{}}}`)

	result := reply["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "claude-opus-5") {
		t.Errorf("the answer does not name the model:\n%s", text)
	}
	if !strings.Contains(text, "3.00") {
		t.Errorf("the answer does not carry the price:\n%s", text)
	}
}

func TestMCPUsageToolReadsTheCallersOwnRows(t *testing.T) {
	gw := mcpGateway(t)
	defer gw.Close()

	srv := &MCPServer{BaseURL: gw.URL, APIKey: "bk_live_K"}
	reply := ask(t, srv, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"brewkeg_usage","arguments":{"range":"30d"}}}`)

	result := reply["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "1,234") {
		t.Errorf("the answer does not carry the request count:\n%s", text)
	}
	if !strings.Contains(text, "$12.50") {
		t.Errorf("the answer does not carry the estimate:\n%s", text)
	}
}

// A tool that needs an account must say so, not fail silently.
func TestMCPUsageToolWithoutAKeyExplainsItself(t *testing.T) {
	gw := mcpGateway(t)
	defer gw.Close()

	srv := &MCPServer{BaseURL: gw.URL}
	reply := ask(t, srv, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"brewkeg_usage","arguments":{}}}`)

	result := reply["result"].(map[string]any)
	if result["isError"] != true {
		t.Errorf("the call should be marked as failed: %v", result)
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "brewkeg login") {
		t.Errorf("the answer does not say what to do:\n%s", text)
	}
}

// A notification gets no reply. Answering one would desynchronise the
// stream.
func TestMCPNotificationIsNotAnswered(t *testing.T) {
	srv := &MCPServer{BaseURL: "https://brewkeg.dev"}
	if reply := ask(t, srv, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); reply != nil {
		t.Errorf("a notification was answered: %v", reply)
	}
}

func TestMCPUnknownMethodIsAnError(t *testing.T) {
	srv := &MCPServer{BaseURL: "https://brewkeg.dev"}
	reply := ask(t, srv, `{"jsonrpc":"2.0","id":6,"method":"nope/nope"}`)

	errObj, ok := reply["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected an error, got %v", reply)
	}
	if errObj["code"].(float64) != -32601 {
		t.Errorf("code = %v, want -32601", errObj["code"])
	}
}

// A line that is not a message must not take the server down: one bad line
// should not end the session.
func TestMCPSurvivesAnUnreadableLine(t *testing.T) {
	srv := &MCPServer{BaseURL: "https://brewkeg.dev"}
	in := strings.NewReader("not json at all\n" + `{"jsonrpc":"2.0","id":7,"method":"ping"}` + "\n")
	var out bytes.Buffer
	srv.In = in
	srv.Out = &out
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"id":7`) {
		t.Errorf("the server stopped at the bad line: %q", out.String())
	}
}
