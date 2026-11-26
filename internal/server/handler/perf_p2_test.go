package handler

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wii/senv/internal/server/store"
)

func TestServerTimingHeader(t *testing.T) {
	st := newFakeStore()
	_, token := st.addClientToken("alice", "laptop", "active")
	s := New(st)

	if rec := doRequest(t, s, "PUT", "/v1/vaults/main/metadata", token, map[string]any{
		"blob": []byte("meta"),
	}); rec.Code != http.StatusOK {
		t.Fatalf("put metadata = %d", rec.Code)
	} else if timing := rec.Header().Get("Server-Timing"); !strings.Contains(timing, "auth;dur=") || !strings.Contains(timing, "total;dur=") {
		t.Fatalf("Server-Timing = %q, want auth;dur= and total;dur=", timing)
	}
}

func TestAsyncAccessLogBufferDrainAndDrop(t *testing.T) {
	gate := make(chan struct{})
	var recorded atomic.Int32
	base := newFakeStore()
	_, token := base.addClientToken("alice", "laptop", "active")
	st := &blockingAccessStore{
		fakeStore: base,
		gate:      gate,
		onRecord:  func() { recorded.Add(1) },
	}

	s := New(st, Options{AsyncAccessLogBuffer: 2})
	// First request enters RecordAccess and blocks on gate; next 2 fill buffer; 4th drops without blocking!
	for i := 0; i < 4; i++ {
		start := time.Now()
		rec := doRequest(t, s, "GET", "/v1/vaults/main/metadata", token, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("req %d code = %d, want 404", i, rec.Code)
		}
		if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
			t.Fatalf("req %d blocked on slow RecordAccess for %v", i, elapsed)
		}
	}

	close(gate)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.Shutdown(ctx)

	if got := recorded.Load(); got < 2 || got > 3 {
		t.Fatalf("recorded = %d, want 2..3 (at least 1 dropped when buffer=2)", got)
	}
}

type blockingAccessStore struct {
	*fakeStore
	gate     chan struct{}
	onRecord func()
}

func (b *blockingAccessStore) RecordAccess(ctx context.Context, e store.AccessEvent) error {
	<-b.gate
	if b.onRecord != nil {
		b.onRecord()
	}
	return b.fakeStore.RecordAccess(ctx, e)
}
