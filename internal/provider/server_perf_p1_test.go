package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCollectWithStateSkipsUnchangedReads(t *testing.T) {
	fs := newFakeServer()
	p, cache := newTestProvider(t, fs)
	writeEnvVar(t, cache, "default", "K1", "val1")
	writeEnvVar(t, cache, "default", "K2", "val2")

	if _, err := p.push(context.Background()); err != nil {
		t.Fatalf("initial push: %v", err)
	}

	// Simulate a brand-new CLI process by creating a fresh localCache (empty in-process collectSnap).
	fresh := &localCache{configPath: cache.configPath, dataPath: cache.dataPath, binding: cache.binding, openRoot: cache.openRoot}
	st, err := fresh.loadState()
	if err != nil {
		t.Fatal(err)
	}
	entries, _, reads, err := fresh.collectCachedWithState(st)
	if err != nil {
		t.Fatal(err)
	}
	if reads != 0 {
		t.Fatalf("warm stat collect read %d files, want 0", reads)
	}
	if len(p.collectDirty(st, entries)) != 0 {
		t.Fatalf("expected 0 dirty entries on warm stat collect")
	}

	// Modify K1 -> stat changes -> 1 read, 1 dirty entry.
	time.Sleep(5 * time.Millisecond)
	writeEnvVar(t, fresh, "default", "K1", "val1-updated")
	entries2, _, reads2, err := fresh.collectCachedWithState(st)
	if err != nil {
		t.Fatal(err)
	}
	if reads2 != 1 {
		t.Fatalf("after 1 file edit, reads = %d, want 1", reads2)
	}
	dirty := p.collectDirty(st, entries2)
	if len(dirty) != 1 || dirty[0].Key != "K1" {
		t.Fatalf("unexpected dirty after edit: %+v", dirty)
	}
}

func TestPullEmptyFastPathSkipsMetadataAndStateRewrite(t *testing.T) {
	fs := newFakeServer()
	p, cache := newTestProvider(t, fs)
	writeEnvVar(t, cache, "default", "K1", "val1")
	if _, err := p.push(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Wrap fakeServer with PullWithMeta returning matching metadata_hash and count GetMetadata calls.
	metaHash := hashBytes(fs.metadata["main"])
	var getMetaCalls int
	api := &metaHashCountingServer{
		fakeServer: fs,
		metaHash:   metaHash,
		onGetMeta:  func() { getMetaCalls++ },
	}
	p2 := newServerProvider(api, cache.configPath, cache.dataPath, "main")

	statePath := filepath.Join(cache.dataPath, syncStateFileName)
	beforeInfo, err := os.Stat(statePath)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(15 * time.Millisecond)
	res, err := p2.pull(context.Background())
	if err != nil {
		t.Fatalf("empty pull: %v", err)
	}
	if res.Applied != 0 || getMetaCalls != 0 {
		t.Fatalf("expected 0 applied and 0 GetMetadata calls, got applied=%d getMeta=%d", res.Applied, getMetaCalls)
	}

	afterInfo, err := os.Stat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatalf("empty pull rewrote sync-state.json unnecessarily: %v -> %v", beforeInfo.ModTime(), afterInfo.ModTime())
	}
	if _, err := os.Stat(filepath.Join(cache.dataPath, lastPullFileName)); err != nil {
		t.Fatalf("expected .senv-last-pull to be written: %v", err)
	}
}

type metaHashCountingServer struct {
	*fakeServer
	metaHash  string
	onGetMeta func()
}

func (m *metaHashCountingServer) GetMetadata(ctx context.Context, vault string) ([]byte, error) {
	if m.onGetMeta != nil {
		m.onGetMeta()
	}
	return m.fakeServer.GetMetadata(ctx, vault)
}

func (m *metaHashCountingServer) PullWithMeta(ctx context.Context, vault string, since int64, _ bool) ([]Entry, int64, string, error) {
	entries, rev, err := m.fakeServer.Pull(ctx, vault, since)
	return entries, rev, m.metaHash, err
}
