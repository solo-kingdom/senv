package provider

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wii/senv/internal/storage"
)

func TestClientGzipDecodeAndBombGuard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/vaults/main/entries":
			if r.Header.Get("Accept-Encoding") != "gzip" {
				t.Errorf("expected Accept-Encoding: gzip, got %q", r.Header.Get("Accept-Encoding"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "gzip")
			zw := gzip.NewWriter(w)
			_, _ = zw.Write([]byte(`{"entries":[{"kind":"env","grp":"default","key":"K1","ciphertext":"aGVsbG8=","revision":1}],"latest_revision":1,"has_more":false,"next_since":1,"metadata_hash":"abc"}`))
			_ = zw.Close()
		case "/v1/vaults/bomb/entries":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "gzip")
			zw := gzip.NewWriter(w)
			// Write > 64 MiB of zeros (compresses to ~64 KiB on the wire, triggers bomb guard on decode).
			chunk := make([]byte, 1<<20)
			for i := 0; i < 66; i++ {
				if _, err := zw.Write(chunk); err != nil {
					break
				}
			}
			_ = zw.Close()
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newServerClient(srv.URL, "tok")
	entries, rev, hash, err := c.PullWithMeta(context.Background(), "main", 0, false)
	if err != nil {
		t.Fatalf("PullWithMeta gzip: %v", err)
	}
	if len(entries) != 1 || entries[0].Key != "K1" || rev != 1 || hash != "abc" {
		t.Fatalf("unexpected pull result: entries=%+v rev=%d hash=%q", entries, rev, hash)
	}

	_, _, _, err = c.PullWithMeta(context.Background(), "bomb", 0, false)
	if err == nil {
		t.Fatalf("expected gzip bomb error, got nil")
	}
}

func TestClientPullPaginationAndOldServerFallback(t *testing.T) {
	var pages atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/vaults/paged/entries":
			n := pages.Add(1)
			if n == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"entries": []Entry{{Kind: "env", Grp: "default", Key: "K1", Revision: 1}},
					"latest_revision": 2, "has_more": true, "next_since": 1, "metadata_hash": "mh1",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"entries": []Entry{{Kind: "env", Grp: "default", Key: "K2", Revision: 2}},
				"latest_revision": 2, "has_more": false, "next_since": 2, "metadata_hash": "mh1",
			})
		case "/v1/vaults/legacy/entries":
			// Old server response without has_more / next_since / metadata_hash.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"entries":         []Entry{{Kind: "env", Grp: "default", Key: "OLD", Revision: 5}},
				"latest_revision": 5,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := newServerClient(srv.URL, "tok")
	entries, rev, hash, err := c.PullWithMeta(context.Background(), "paged", 0, false)
	if err != nil {
		t.Fatalf("paged PullWithMeta: %v", err)
	}
	if len(entries) != 2 || rev != 2 || hash != "mh1" || pages.Load() != 2 {
		t.Fatalf("unexpected paged result: len=%d rev=%d hash=%q pages=%d", len(entries), rev, hash, pages.Load())
	}

	entriesOld, revOld, hashOld, err := c.PullWithMeta(context.Background(), "legacy", 0, false)
	if err != nil {
		t.Fatalf("legacy PullWithMeta: %v", err)
	}
	if len(entriesOld) != 1 || entriesOld[0].Key != "OLD" || revOld != 5 || hashOld != "" {
		t.Fatalf("unexpected legacy result: entries=%+v rev=%d hash=%q", entriesOld, revOld, hashOld)
	}
}

func TestClientStallTimeoutAndErrorClassification(t *testing.T) {
	// 1) Slow streaming body that sends bytes every 30ms for 180ms total, while readTimeout is 80ms.
	//    Must succeed because the stream never stalls.
	// 2) Stalled body that hangs for 300ms mid-body with readTimeout=80ms -> IsTimeout=true, IsUnreachable=false.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/vaults/slow-stream/entries":
			_, _ = io.WriteString(w, `{"entries":[`)
			if flusher != nil {
				flusher.Flush()
			}
			for i := 0; i < 6; i++ {
				time.Sleep(30 * time.Millisecond)
				_, _ = io.WriteString(w, ` `)
				if flusher != nil {
					flusher.Flush()
				}
			}
			_, _ = io.WriteString(w, `],"revision":1}`)
		case "/v1/vaults/stalled/entries":
			_, _ = io.WriteString(w, `{"entries":[`)
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(300 * time.Millisecond)
		}
	}))
	defer srv.Close()

	c := newServerClientWithTimeout(srv.URL, "tok", 80*time.Millisecond)
	if _, _, err := c.Pull(context.Background(), "slow-stream", 0); err != nil {
		t.Fatalf("slow-stream failed despite continuous progress: %v", err)
	}

	_, _, err := c.Pull(context.Background(), "stalled", 0)
	if err == nil {
		t.Fatalf("expected stalled pull to fail")
	}
	if !IsTimeout(err) {
		t.Fatalf("expected IsTimeout(err)=true, got false: %v", err)
	}
	if IsUnreachable(err) {
		t.Fatalf("expected IsUnreachable(err)=false for timeout, got true: %v", err)
	}

	// Closed port -> IsUnreachable=true, IsTimeout=false.
	closedSrv := httptest.NewServer(http.NewServeMux())
	closedURL := closedSrv.URL
	closedSrv.Close()
	cClosed := newServerClientWithTimeout(closedURL, "tok", 500*time.Millisecond)
	_, _, errConn := cClosed.Pull(context.Background(), "main", 0)
	if errConn == nil {
		t.Fatalf("expected connection refused error")
	}
	if !IsUnreachable(errConn) || IsTimeout(errConn) {
		t.Fatalf("unexpected classification for closed server: timeout=%v unreachable=%v err=%v", IsTimeout(errConn), IsUnreachable(errConn), errConn)
	}
}

func TestClientLookupFallbackVsVaultNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/vaults/old/entries/lookup":
			http.NotFound(w, r) // plain text 404 from old router
		case "/v1/vaults/missing/entries/lookup":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"vault not found"}`)
		}
	}))
	defer srv.Close()

	c := newServerClient(srv.URL, "tok")
	_, _, errOld := c.Lookup(context.Background(), "old", []EntryKey{{Kind: "env", Grp: "default", Key: "A"}}, false)
	if !errors.Is(errOld, ErrLookupUnsupported) {
		t.Fatalf("old router 404 = %v, want ErrLookupUnsupported", errOld)
	}

	_, _, errMissing := c.Lookup(context.Background(), "missing", []EntryKey{{Kind: "env", Grp: "default", Key: "A"}}, false)
	if !errors.Is(errMissing, ErrVaultNotFound) {
		t.Fatalf("vault 404 = %v, want ErrVaultNotFound", errMissing)
	}
}

func TestHealFalseConflictsZeroRequestAndLookupFallback(t *testing.T) {
	fs := newFakeServer()
	p, cache := newTestProvider(t, fs)
	writeEnvVar(t, cache, "default", "A", "val-a")

	localEntries, err := cache.collect()
	if err != nil {
		t.Fatal(err)
	}
	var target Entry
	for _, e := range localEntries {
		if e.Kind == KindEnv && e.Grp == "default" && e.Key == "A" {
			target = e
			break
		}
	}
	if target.Key == "" {
		t.Fatal("missing env/default/A local entry")
	}

	cipherSum := sha256.Sum256(target.Ciphertext)
	cipherHash := hex.EncodeToString(cipherSum[:])
	target.Revision = 7
	fs.entries["main"] = map[string]Entry{entryID(target.Kind, target.Grp, target.Key): target}
	fs.seq["main"] = 7

	// 1) Conflict includes CiphertextSHA256 -> heals with 0 Pull/Lookup calls.
	var pullCalls, lookupCalls int
	api := &countingLookupServer{
		fakeServer: fs,
		onPull:     func() { pullCalls++ },
		onLookup:   func() { lookupCalls++ },
	}
	pCount := newServerProvider(api, p.cache.configPath, p.cache.dataPath, "main")
	healedCount, remainingDirty, remainingConflicts, healedStates, err := pCount.healFalseConflicts(
		context.Background(),
		[]Entry{target},
		[]Conflict{{Kind: KindEnv, Grp: "default", Key: "A", CurrentRevision: 7, CiphertextSHA256: cipherHash}},
	)
	if err != nil {
		t.Fatalf("healFalseConflicts sha256: %v", err)
	}
	if healedCount != 1 || len(remainingDirty) != 0 || len(remainingConflicts) != 0 || len(healedStates) != 1 || pullCalls != 0 || lookupCalls != 0 {
		t.Fatalf("expected zero-request heal: healed=%d dirty=%d conflicts=%d states=%d pulls=%d lookups=%d",
			healedCount, len(remainingDirty), len(remainingConflicts), len(healedStates), pullCalls, lookupCalls)
	}

	// 2) Conflict lacks CiphertextSHA256 -> uses Lookup, NOT Pull(0).
	healedCount2, remainingDirty2, remainingConflicts2, healedStates2, err := pCount.healFalseConflicts(
		context.Background(),
		[]Entry{target},
		[]Conflict{{Kind: KindEnv, Grp: "default", Key: "A", CurrentRevision: 7}},
	)
	if err != nil {
		t.Fatalf("healFalseConflicts lookup: %v", err)
	}
	if healedCount2 != 1 || len(remainingDirty2) != 0 || len(remainingConflicts2) != 0 || len(healedStates2) != 1 || pullCalls != 0 || lookupCalls != 1 {
		t.Fatalf("expected lookup heal without pull: healed=%d dirty=%d conflicts=%d states=%d pulls=%d lookups=%d",
			healedCount2, len(remainingDirty2), len(remainingConflicts2), len(healedStates2), pullCalls, lookupCalls)
	}
}

func TestPushAndForcePushLockFreeNetworkIO(t *testing.T) {
	fs := newFakeServer()
	p, cache := newTestProvider(t, fs)
	writeEnvVar(t, cache, "default", "K1", "v1")

	pushStarted := make(chan struct{})
	releasePush := make(chan struct{})
	fs.beforePush = func() {
		select {
		case <-pushStarted:
		default:
			close(pushStarted)
		}
		<-releasePush
	}

	pushDone := make(chan error, 1)
	go func() {
		_, err := p.push(context.Background())
		pushDone <- err
	}()

	<-pushStarted
	// Concurrent LoadMetadata must complete quickly while Push is blocked on network I/O.
	start := time.Now()
	if _, err := storage.NewManager(cache.configPath, cache.dataPath).LoadMetadata(); err != nil {
		t.Fatalf("concurrent LoadMetadata failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("LoadMetadata blocked behind Push network I/O for %v", elapsed)
	}

	// Concurrent local write to K1 during push network window must stay dirty after push commits.
	writeEnvVar(t, cache, "default", "K1", "v2-during-push")

	close(releasePush)
	if err := <-pushDone; err != nil {
		t.Fatalf("Push: %v", err)
	}

	state, err := cache.loadState()
	if err != nil {
		t.Fatal(err)
	}
	localAfter, err := cache.collect()
	if err != nil {
		t.Fatal(err)
	}
	var k1Hash string
	for _, e := range localAfter {
		if e.Kind == KindEnv && e.Grp == "default" && e.Key == "K1" {
			k1Hash = hashBytes(e.Ciphertext)
			break
		}
	}
	if stEntry := state.Entries[entryID(KindEnv, "default", "K1")]; stEntry.Hash == k1Hash {
		t.Fatalf("concurrent local write during push was overwritten as clean in state: %+v", stEntry)
	}

	// Also verify ForcePush does not hold vault lock over network I/O.
	forceStarted := make(chan struct{})
	releaseForce := make(chan struct{})
	fs.beforePush = func() {
		select {
		case <-forceStarted:
		default:
			close(forceStarted)
		}
		<-releaseForce
	}
	forceDone := make(chan error, 1)
	go func() {
		forceDone <- p.ForcePush(context.Background())
	}()
	<-forceStarted
	startForce := time.Now()
	if _, err := storage.NewManager(cache.configPath, cache.dataPath).LoadMetadata(); err != nil {
		t.Fatalf("concurrent LoadMetadata during ForcePush failed: %v", err)
	}
	if elapsed := time.Since(startForce); elapsed > 200*time.Millisecond {
		t.Fatalf("LoadMetadata blocked behind ForcePush network I/O for %v", elapsed)
	}
	close(releaseForce)
	if err := <-forceDone; err != nil {
		t.Fatalf("ForcePush: %v", err)
	}
}

type countingLookupServer struct {
	*fakeServer
	onPull   func()
	onLookup func()
}

func (c *countingLookupServer) Pull(ctx context.Context, vault string, since int64) ([]Entry, int64, error) {
	if c.onPull != nil {
		c.onPull()
	}
	return c.fakeServer.Pull(ctx, vault, since)
}

func (c *countingLookupServer) Lookup(_ context.Context, vault string, keys []EntryKey, metaOnly bool) ([]Entry, int64, error) {
	if c.onLookup != nil {
		c.onLookup()
	}
	var out []Entry
	for _, k := range keys {
		if e, ok := c.fakeServer.entries[vault][entryID(k.Kind, k.Grp, k.Key)]; ok {
			if metaOnly {
				e.Size = int64(len(e.Ciphertext))
				sum := sha256.Sum256(e.Ciphertext)
				e.CiphertextSHA256 = hex.EncodeToString(sum[:])
				e.Ciphertext = nil
			}
			out = append(out, e)
		}
	}
	return out, c.fakeServer.seq[vault], nil
}

// Keep bytes import used if needed.
var _ = bytes.Equal
