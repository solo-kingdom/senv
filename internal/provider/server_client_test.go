package provider

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestServerClientRefusesRedirects(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/steal", http.StatusFound)
	}))
	defer redirector.Close()

	client := newServerClient(redirector.URL, "secret-bearer-token")
	_, err := client.GetMetadata(context.Background(), "main")
	if err == nil {
		t.Fatal("expected redirect to be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "重定向") {
		t.Fatalf("unexpected error: %v", err)
	}
	if hits := targetHits.Load(); hits != 0 {
		t.Fatalf("redirect target received %d request(s), want 0 (Bearer token leak)", hits)
	}
}

func TestDecodeBoundedJSONRejectsOversizedBody(t *testing.T) {
	payload := []byte(`{"blob":"` + strings.Repeat("a", 128) + `"}`)
	var out struct {
		Blob string `json:"blob"`
	}
	if err := decodeBoundedJSON(bytes.NewReader(payload), 64, &out); err == nil {
		t.Fatal("expected oversized response to fail")
	}
	if err := decodeBoundedJSON(bytes.NewReader(payload), int64(len(payload)), &out); err != nil {
		t.Fatalf("exact-size response failed: %v", err)
	}
}
