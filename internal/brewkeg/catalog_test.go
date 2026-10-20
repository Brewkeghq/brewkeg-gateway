package brewkeg

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/catalog" {
			t.Errorf("path = %q, want /api/catalog", r.URL.Path)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"id":"claude-opus-5","name":"claude-opus-5","provider":"Anthropic","tier":"max","inputUsd":3,"outputUsd":15,"cacheReadUsd":0.3,"cacheWriteUsd":3.75,"discount":40,"contextWindow":1000000,"maxOutput":8192,"capabilities":["text"]}]}`))
	}))
	defer srv.Close()

	cat, err := FetchCatalog(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("FetchCatalog: %v", err)
	}
	if len(cat.Models) != 1 {
		t.Fatalf("models = %d, want 1", len(cat.Models))
	}
	m, ok := cat.ByID("claude-opus-5")
	if !ok {
		t.Fatal("claude-opus-5 missing from the catalogue")
	}
	if m.InputUSD != 3 || m.OutputUSD != 15 {
		t.Errorf("prices = %v/%v, want 3/15", m.InputUSD, m.OutputUSD)
	}
}

func TestFetchCatalogEmptyIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer srv.Close()
	if _, err := FetchCatalog(context.Background(), srv.URL); err == nil {
		t.Fatal("an empty catalogue must be an error, not an empty list")
	}
}

func TestFetchUsageSendsTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "Bearer bk_live_x" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("last"); got != "30d" {
			t.Errorf("last = %q, want 30d", got)
		}
		_, _ = w.Write([]byte(`{"range":"30d","totals":{"requests":2,"tokensIn":10,"tokensOut":5},"byModel":[],"daily":[]}`))
	}))
	defer srv.Close()

	u, err := FetchUsage(context.Background(), srv.URL, "bk_live_x", "30d")
	if err != nil {
		t.Fatalf("FetchUsage: %v", err)
	}
	if u.Totals.Requests != 2 {
		t.Errorf("requests = %d, want 2", u.Totals.Requests)
	}
}

func TestFetchUsageRejectedKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	_, err := FetchUsage(context.Background(), srv.URL, "bk_live_x", "7d")
	if err == nil {
		t.Fatal("a rejected key must be an error")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("error = %q, want it to say the key was rejected", err)
	}
}

func TestClaudeFamilyPicksTheShortest(t *testing.T) {
	cat := Catalog{Models: []CatalogModel{
		{ID: "claude-opus-5-5"}, {ID: "claude-opus-5"}, {ID: "claude-sonnet-5"},
	}}
	m, ok := cat.ClaudeFamily("opus")
	if !ok {
		t.Fatal("no opus model")
	}
	if m.ID != "claude-opus-5" {
		t.Errorf("opus = %q, want claude-opus-5", m.ID)
	}
	if _, ok := cat.ClaudeFamily("haiku"); ok {
		t.Error("haiku must not be found in a catalogue that has none")
	}
}

func TestLatestNpmVersion(t *testing.T) {
	if got := LatestNpmVersion(context.Background(), ""); got != "" {
		t.Errorf("an empty package must not look up a version, got %q", got)
	}
}
