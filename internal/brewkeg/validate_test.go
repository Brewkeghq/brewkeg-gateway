package brewkeg

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeGateway(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("expected a probe on /v1/messages, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer bk_") {
			t.Errorf("probe did not send the key as a bearer token: %q", got)
		}
		w.WriteHeader(status)
		if body != "" {
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCheckKeyGoodKey(t *testing.T) {
	srv := fakeGateway(t, 200, `{"id":"msg_1"}`)
	got := CheckKey(context.Background(), srv.URL, "bk_live_good")
	if !got.OK || !got.Valid {
		t.Fatalf("a 200 must count as a working key: %+v", got)
	}
	if !strings.Contains(got.Message, "works") {
		t.Errorf("message should say it works, got %q", got.Message)
	}
}

func TestCheckKeyRejected(t *testing.T) {
	srv := fakeGateway(t, 401, `{"error":{"message":"invalid api key"}}`)
	got := CheckKey(context.Background(), srv.URL, "bk_live_bad")
	if got.Valid || got.OK {
		t.Fatalf("a 401 must not be reported as valid: %+v", got)
	}
	if !strings.Contains(got.Message, "rejected") {
		t.Errorf("message should say it was rejected, got %q", got.Message)
	}
}

// The trap: a 429 means the gateway authenticated the key and then refused on
// quota. Telling the user their key is invalid here loses their config setup.
func TestCheckKeyOutOfQuotaIsStillValid(t *testing.T) {
	srv := fakeGateway(t, 429, `{"error":{"message":"token quota exceeded for this month"}}`)
	got := CheckKey(context.Background(), srv.URL, "bk_live_broke")
	if !got.Valid {
		t.Fatalf("429 means the key authenticated: %+v", got)
	}
	if got.OK {
		t.Error("a 429 key cannot run right now, so OK must stay false")
	}
	if !strings.Contains(got.Message, "out of quota") || !strings.Contains(got.Message, "this month") {
		t.Errorf("expected a quota message with the reason, got %q", got.Message)
	}
}

func TestCheckKeyEmptyKeyDoesNotCallOut(t *testing.T) {
	got := CheckKey(context.Background(), "http://127.0.0.1:1", "")
	if got.Valid || got.OK {
		t.Fatalf("an empty key is never valid: %+v", got)
	}
	if got.Status != 0 {
		t.Errorf("an empty key must short-circuit before any request, got status %d", got.Status)
	}
}

// Probing with an expensive model would cost the user money just to check a key.
func TestCheckKeyUsesTheCheapestModel(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		body = buf[:n]
		w.WriteHeader(200)
	}))
	defer srv.Close()

	CheckKey(context.Background(), srv.URL, "bk_live_good")
	if !strings.Contains(string(body), checkModel) {
		t.Errorf("probe should use %s, got %s", checkModel, body)
	}
	if !strings.Contains(string(body), `"max_tokens":1`) {
		t.Errorf("probe must ask for a single token, got %s", body)
	}
}

// Only a definitive rejection may stop a config write. Every other verdict —
// including the ones the key is fine in — must let the write through, or the
// app refuses to work on a bad network day.
func TestOnlyRejectionBlocksAWrite(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		wantBlk  bool
		wantRech bool
		wantMsg  string
	}{
		{"works", 200, false, true, ""},
		{"rejected", 401, true, true, "rejected"},
		{"forbidden", 403, true, true, "rejected"},
		{"out of quota", 429, false, true, "valid"},
		{"probe refused", 400, false, true, "valid"},
		// The gateway is down. This is the case the old two-state check got
		// catastrophically wrong: it reported "check the key" for a 502.
		{"gateway 500", 500, false, true, "trouble"},
		{"gateway 502", 502, false, true, "trouble"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := fakeGateway(t, tc.status, `{"error":{"message":"nope"}}`)
			got := CheckKey(context.Background(), srv.URL, "bk_live_x")
			if got.Blocked() != tc.wantBlk {
				t.Errorf("Blocked() = %v, want %v (message: %s)", got.Blocked(), tc.wantBlk, got.Message)
			}
			if got.Reachable != tc.wantRech {
				t.Errorf("Reachable = %v, want %v", got.Reachable, tc.wantRech)
			}
			if tc.wantMsg != "" && !strings.Contains(got.Message, tc.wantMsg) {
				t.Errorf("message %q should mention %q", got.Message, tc.wantMsg)
			}
		})
	}
}

// Unreachable is not reachable-and-satisfied: no HTTP response means no verdict
// on the key at all.
func TestUnreachableGatewayDoesNotBlameTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	got := CheckKey(context.Background(), url, "bk_live_x")
	if got.Reachable {
		t.Error("Reachable should be false when the request never landed")
	}
	if got.Blocked() {
		t.Errorf("a dead gateway must never be reported as a bad key: %s", got.Message)
	}
	if !strings.Contains(got.Message, "Could not reach") {
		t.Errorf("message = %q, want it to say we could not reach brewkeg", got.Message)
	}
}

func TestEmptyKeyIsNeitherValidNorBlocked(t *testing.T) {
	got := CheckKey(context.Background(), "https://brewkeg.invalid", "")
	if got.Blocked() {
		t.Error("an empty key must not be reported as rejected — nothing was sent")
	}
	if got.Message != "Paste your API key." {
		t.Errorf("message = %q", got.Message)
	}
}
