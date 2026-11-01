package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/brewkeghq/brewkeg-gateway/internal/brewkeg"
)

func TestStripFlags(t *testing.T) {
	got := stripFlags([]string{"--model", "gpt-5.5", "fix", "the", "bug"}, []string{"--model"}, []string{"--print-env"})
	want := []string{"fix", "the", "bug"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
		}
	}
}

func TestStripFlagsLeavesTheRest(t *testing.T) {
	got := stripFlags([]string{"--print-env", "hello"}, []string{"--model"}, []string{"--print-env"})
	if len(got) != 1 || got[0] != "hello" {
		t.Errorf("got %v, want [hello]", got)
	}
}

func TestClaudeSettingsJSON(t *testing.T) {
	s := session{
		APIKey:  "bk_live_x",
		BaseURL: "https://brewkeg.dev",
		Model:   "claude-opus-5",
		Catalog: brewkeg.Catalog{Models: []brewkeg.CatalogModel{
			{ID: "claude-opus-5", ContextWindow: 1_000_000, InputUSD: 3, OutputUSD: 15},
			{ID: "claude-haiku-4-5", ContextWindow: 200_000, InputUSD: 1, OutputUSD: 5},
			{ID: "gpt-5.5", ContextWindow: 400_000, InputUSD: 0.5, OutputUSD: 3},
		}},
	}
	raw, err := claudeSettingsJSON(s)
	if err != nil {
		t.Fatalf("claudeSettingsJSON: %v", err)
	}
	var doc struct {
		Env   map[string]string `json:"env"`
		Extra map[string]json.RawMessage
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the settings file is not valid JSON: %v", err)
	}
	if doc.Env["ANTHROPIC_BASE_URL"] != "https://brewkeg.dev" {
		t.Errorf("base url = %q", doc.Env["ANTHROPIC_BASE_URL"])
	}
	if doc.Env["ANTHROPIC_AUTH_TOKEN"] != "bk_live_x" {
		t.Errorf("token = %q", doc.Env["ANTHROPIC_AUTH_TOKEN"])
	}
	if _, ok := doc.Env["ANTHROPIC_API_KEY"]; ok {
		t.Error("ANTHROPIC_API_KEY must not be set at all, not even empty")
	}
	if doc.Env["ANTHROPIC_MODEL"] != "claude-opus-5" {
		t.Errorf("model = %q", doc.Env["ANTHROPIC_MODEL"])
	}
	if doc.Env["ANTHROPIC_SMALL_FAST_MODEL"] != "claude-haiku-4-5" {
		t.Errorf("fast = %q", doc.Env["ANTHROPIC_SMALL_FAST_MODEL"])
	}

	// The picker is the user's. `brewkeg launch` sets an endpoint and a model
	// for the session; it does not get to rewrite the list of models the tool
	// offers, and it does not get to retarget the built-in rows either.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	if _, ok := top["modelPicker"]; ok {
		t.Errorf("launch wrote a modelPicker block:\n%s", raw)
	}
	for key := range doc.Env {
		if strings.HasPrefix(key, "ANTHROPIC_DEFAULT_") {
			t.Errorf("launch set %s, which retargets a built-in picker row", key)
		}
	}
}

func TestParseVersion(t *testing.T) {
	if got := parseVersion("v0.2.2"); got != [3]int{0, 2, 2} {
		t.Errorf("got %v", got)
	}
	if got := parseVersion("cli-v1.2"); got != [3]int{1, 2, 0} {
		t.Errorf("got %v", got)
	}
	if !newerVersion("0.2.3", "0.2.2") {
		t.Error("0.2.3 is newer than 0.2.2")
	}
	if newerVersion("0.2.2", "0.2.3") {
		t.Error("0.2.2 is not newer than 0.2.3")
	}
	if !newerVersion("0.3.0", "0.2.9") {
		t.Error("0.3.0 is newer than 0.2.9")
	}
}

func TestAgentByID(t *testing.T) {
	if _, ok := brewkeg.AgentByID("command-code"); !ok {
		t.Error("command-code is missing")
	}
	if a, ok := brewkeg.AgentByID("cmd"); !ok || a.ID != "command-code" {
		t.Error("cmd must resolve to command-code")
	}
	if _, ok := brewkeg.AgentByID("nope"); ok {
		t.Error("an unknown agent must not resolve")
	}
}

func TestOpencodeLaunchUsesTheFastModel(t *testing.T) {
	s := session{
		APIKey:  "bk_live_x",
		BaseURL: "https://brewkeg.dev",
		Model:   "gpt-5.5",
		Catalog: brewkeg.Catalog{Models: []brewkeg.CatalogModel{
			{ID: "claude-haiku-4-5", ContextWindow: 200_000},
			{ID: "gpt-5.5", ContextWindow: 400_000},
		}},
	}
	p, err := opencodeLaunch(s, nil, "opencode")
	if err != nil {
		t.Fatalf("opencodeLaunch: %v", err)
	}
	if len(p.env) != 1 || !strings.HasPrefix(p.env[0], "OPENCODE_CONFIG_CONTENT=") {
		t.Fatalf("env = %v", p.env)
	}
	var doc struct {
		Model      string `json:"model"`
		SmallModel string `json:"small_model"`
	}
	raw := strings.TrimPrefix(p.env[0], "OPENCODE_CONFIG_CONTENT=")
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("config is not valid JSON: %v", err)
	}
	if doc.Model != "brewkeg/gpt-5.5" {
		t.Errorf("model = %q", doc.Model)
	}
	if doc.SmallModel == doc.Model {
		t.Error("the small model must not be the model itself")
	}
}

func TestDropEnv(t *testing.T) {
	env := []string{"PATH=/bin", "ANTHROPIC_API_KEY=sk-real", "HOME=/root"}
	got := dropEnv(env, []string{"ANTHROPIC_API_KEY"})
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	for _, kv := range got {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY") {
			t.Errorf("%s survived", kv)
		}
	}
}
