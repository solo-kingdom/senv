package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAlertRenderBodyEscapesAndValidatesJSON(t *testing.T) {
	d := newAlertDetector(nil, "https://example.com/hook", 3, 0, time.Minute,
		`{"text":"{{.alert}} ip={{.ip}} reason={{.reason}} client={{.client}}"}`)
	if d.bodyTmpl == nil {
		t.Fatal("expected valid template")
	}
	body, err := d.renderBody(alertPayload{
		Alert:  "client_blocked",
		IP:     "1.2.3.4",
		Reason: "bad \"quoted\"\n\\escape",
		Client: "dev\"box",
	})
	if err != nil {
		t.Fatalf("renderBody failed: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("rendered body is invalid JSON: %v\n%s", err, string(body))
	}
	if !strings.Contains(parsed["text"].(string), `bad "quoted"`) {
		t.Fatalf("unexpected rendered text: %q", parsed["text"])
	}
}

func TestHandleRegisterLimitsBodyAndRejectsQuotes(t *testing.T) {
	st := newFakeStore()
	s := New(st, Options{AuthRateLimit: 2})

	// Oversized body (> 4 KiB) returns 413 and counts toward rate limit.
	big := `{"code":"abc","name":"` + strings.Repeat("a", MaxRegisterBodyBytes+100) + `"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/register", strings.NewReader(big))
	rec1 := httptest.NewRecorder()
	s.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized register status = %d, want 413", rec1.Code)
	}

	// Name with quote returns 400 and triggers rate limit on 2nd failure -> 3rd gets 429.
	req2 := httptest.NewRequest(http.MethodPost, "/v1/register", bytes.NewBufferString(`{"code":"abc","name":"bad\"name"}`))
	rec2 := httptest.NewRecorder()
	s.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("quoted name status = %d, want 400", rec2.Code)
	}

	req3 := httptest.NewRequest(http.MethodPost, "/v1/register", bytes.NewBufferString(`{"code":"abc","name":"ok"}`))
	rec3 := httptest.NewRecorder()
	s.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after 2 register failures, got %d", rec3.Code)
	}
}

func TestSanitizeLogFieldStripsControlSequences(t *testing.T) {
	got := sanitizeLogField("/v1/\x1b[31mevil\x07\npath\x9b")
	if strings.ContainsAny(got, "\x1b\x07\n\x9b") {
		t.Fatalf("control chars survived sanitizeLogField: %q", got)
	}
	if got != "/v1/[31mevilpath" {
		t.Fatalf("sanitizeLogField = %q", got)
	}
}
