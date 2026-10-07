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
