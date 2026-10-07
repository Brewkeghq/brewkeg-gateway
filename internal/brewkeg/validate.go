package brewkeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// KeyCheck is the result of testing an API key against the live gateway. The
// distinction that matters to the user is Valid (the key authenticated) versus
// usable (it authenticated AND has room to run) — a key with no credit left is
// still the right key, it just cannot make a request yet.
type KeyCheck struct {
	OK        bool   `json:"ok"`    // the key works right now
	Valid     bool   `json:"valid"` // the gateway accepted it
	Status    int    `json:"status"`
	Message   string `json:"message"`
	LatencyMS int64  `json:"latencyMs"`

	// Rejected is the only verdict that must stop a write: the gateway saw
	// the key and refused it. Baking a rejected key into a tool config breaks
	// that tool until someone edits the file by hand.
	Rejected bool `json:"rejected"`

	// Reachable says we got an answer at all. False means the request never
	// landed — no wifi, DNS, gateway down, a 502 from the edge. That is NOT
	// evidence about the key, so it must never be reported as a bad key.
	Reachable bool `json:"reachable"`
}

// Blocked reports whether this verdict must stop a config write. Only a
// definitive rejection blocks; "we could not ask" is not the same answer.
func (k KeyCheck) Blocked() bool { return k.Rejected }

// checkModel is the cheapest catalog model, so validation costs a rounding
// error. It is a real catalog id, which the gateway's allowlist accepts.
const checkModel = "claude-haiku-4-5"

// CheckKey sends the smallest possible authenticated request to the gateway.
//
// This has to be a real inference call: GET /v1/models is public and answers
// anyone, so it proves nothing about the key. One token through the cheapest
// model is the only check that proves the key is accepted, the route is live,
// and the account has room.
func CheckKey(ctx context.Context, baseURL, apiKey string) KeyCheck {
	start := time.Now()
	// Reachable starts false and is set the moment any HTTP response arrives.
	res := KeyCheck{Message: "Could not reach brewkeg."}

	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		res.Message = "Paste your API key."
		return res
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	body, _ := json.Marshal(map[string]any{
		"model":      checkModel,
		"max_tokens": 1,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
	})

	url := strings.TrimRight(baseURL, "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return res
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		res.Message = "Could not reach brewkeg. Check your internet connection."
		return res
	}
	defer resp.Body.Close()
	res.Reachable = true

	// Read only what we need; a long error body is not worth parsing.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	res.Status = resp.StatusCode
	res.LatencyMS = time.Since(start).Milliseconds()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		res.OK, res.Valid = true, true
		res.Message = fmt.Sprintf("Key works — %s replied in %dms.", checkModel, res.LatencyMS)

	case resp.StatusCode == 401 || resp.StatusCode == 403:
		res.Rejected = true
		res.Message = "That key was rejected by brewkeg."

	case resp.StatusCode == 429:
		// The gateway authenticated the key and then refused on quota, so the
		// key is good — it just cannot run right now.
		res.Valid = true
		if reason := quotaReason(raw); reason != "" {
			res.Message = "Key is valid, but you are out of quota. " + reason
		} else {
			res.Message = "Key is valid, but you are out of quota."
		}

	case resp.StatusCode == 400:
		// A rejected body means the key got far enough to be authenticated but
		// our probe shape was refused. Treat it as valid, not as a bad key.
		res.Valid = true
		res.Message = "Key is valid, but the check request was refused by the gateway."

	case resp.StatusCode >= 500:
		// The gateway is broken, the key is not. Never blame the key for this.
		res.Message = fmt.Sprintf("brewkeg is having trouble (%d). Nothing was changed — try again in a moment.", resp.StatusCode)

	default:
		// An unexpected 4xx. We asked and got an answer, but we do not
		// recognise it as an auth verdict, so we do not treat it as one.
		res.Message = fmt.Sprintf("Gateway returned %d. Check the key and try again.", resp.StatusCode)
	}
	return res
}

// quotaReason pulls the human-readable reason out of the gateway's error body
// when there is one worth showing.
func quotaReason(raw []byte) string {
	var doc struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	msg := doc.Error.Message
	if msg == "" {
		msg = doc.Message
	}
	if len(msg) > 120 {
		msg = msg[:120] + "…"
	}
	return strings.TrimSpace(msg)
}
