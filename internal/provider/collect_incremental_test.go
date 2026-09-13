package provider

import (
	"bytes"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/wii/senv/internal/securefs"
)

type countingReadRoot struct {
	securefs.TrustedRoot
	reads *atomic.Int64
}

func (r *countingReadRoot) Read(segments ...string) ([]byte, error) {
	r.reads.Add(1)
	return r.TrustedRoot.Read(segments...)
}

func installCountingOpener(cache *localCache) *atomic.Int64 {
	reads := &atomic.Int64{}
	inner := cache.rootOpener()
	cache.openRoot = func(path string) (securefs.TrustedRoot, error) {
		root, err := inner(path)
		if err != nil {
			return nil, err
		}
		return &countingReadRoot{TrustedRoot: root, reads: reads}, nil
	}
	return reads
}

func collectMapsEqual(a, b map[string]Entry) bool {
	if len(a) != len(b) {
		return false
	}
	for id, ea := range a {
		eb, ok := b[id]
		if !ok || ea.Kind != eb.Kind || ea.Grp != eb.Grp || ea.Key != eb.Key {
			return false
		}
		if !bytes.Equal(ea.Ciphertext, eb.Ciphertext) {
			return false
		}
	}
	return true
}

func TestCollectIncrementalNoChangeZeroRead(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)
	writeEnvVar(t, cache, "default", "A", "aaa")
	writeEnvVar(t, cache, "default", "B", "bbb")

	first, err := cache.collect()
	if err != nil {
		t.Fatalf("first collect: %v", err)
	}
	if len(first) < 2 {
		t.Fatalf("first collect items = %d, want at least A and B", len(first))
	}

	reads := installCountingOpener(cache)
	second, err := cache.collect()
	if err != nil {
		t.Fatalf("second collect: %v", err)
	}
	if !collectMapsEqual(first, second) {
		t.Fatal("incremental collect without writes diverged from the first snapshot")
	}
	if n := reads.Load(); n != 0 {
		t.Fatalf("unchanged collect issued %d ciphertext reads, want 0", n)
	}
}

func TestCollectIncrementalRereadsOnlyChanged(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)
	writeEnvVar(t, cache, "default", "A", "aaa")
	writeEnvVar(t, cache, "default", "B", "bbb")
	if _, err := cache.collect(); err != nil {
		t.Fatalf("seed collect: %v", err)
	}

	reads := installCountingOpener(cache)
	writeEnvVar(t, cache, "default", "A", "aaa-changed")
	writeEnvVar(t, cache, "prod", "C", "ccc")

	incremental, readsCount, err := cache.collectCached()
	if err != nil {
		t.Fatalf("incremental collect: %v", err)
	}
	if readsCount != 2 {
		t.Fatalf("reads = %d, want 2 (changed A + new C)", readsCount)
	}
	if n := reads.Load(); n != 2 {
		t.Fatalf("opener reads = %d, want 2", n)
	}

	cache.resetCollectCache()
	full, err := cache.collect()
	if err != nil {
		t.Fatalf("full collect: %v", err)
	}
	if !collectMapsEqual(incremental, full) {
		t.Fatal("incremental collect after writes diverged from a full rescan")
	}

	idA := entryID(KindEnv, "default", "A")
	if got := string(incremental[idA].Ciphertext); got != "aaa-changed" {
		t.Fatalf("A ciphertext = %q, want aaa-changed", got)
	}
	idC := entryID(KindEnv, "prod", "C")
	if _, ok := incremental[idC]; !ok {
		t.Fatal("missing new entry C")
	}
}

func TestCollectIncrementalDirtyMatchesFull(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	writeEnvVar(t, cache, "default", "LOCAL", "l1")
	if _, err := cache.collect(); err != nil {
		t.Fatalf("seed collect: %v", err)
	}
	writeEnvVar(t, cache, "default", "LOCAL", "l2")
	writeEnvVar(t, cache, "default", "NEW", "n")

	st, err := cache.loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	inc, err := cache.collect()
	if err != nil {
		t.Fatalf("incremental: %v", err)
	}
	cache.resetCollectCache()
	full, err := cache.collect()
	if err != nil {
		t.Fatalf("full: %v", err)
	}
	got := p.collectDirty(st, inc)
	want := p.collectDirty(st, full)
	if len(got) != len(want) {
		t.Fatalf("dirty incremental = %d, full = %d", len(got), len(want))
	}
	ids := func(entries []Entry) map[string]bool {
		out := make(map[string]bool, len(entries))
		for _, e := range entries {
			out[entryID(e.Kind, e.Grp, e.Key)] = true
		}
		return out
	}
	gotIDs, wantIDs := ids(got), ids(want)
	for id := range wantIDs {
		if !gotIDs[id] {
			t.Errorf("incremental dirty missing %q", id)
		}
	}
}

func TestCollectIncrementalDropsDeleted(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)
	writeEnvVar(t, cache, "default", "GONE", "x")
	if _, err := cache.collect(); err != nil {
		t.Fatalf("seed collect: %v", err)
	}
	path := mustEntryPath(t, cache, KindEnv, "default", "GONE")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	got, err := cache.collect()
	if err != nil {
		t.Fatalf("collect after delete: %v", err)
	}
	if _, ok := got[entryID(KindEnv, "default", "GONE")]; ok {
		t.Fatal("deleted entry still present in incremental snapshot")
	}
}

// TestCollectSkipsMachineLocalArtifacts 验证 dataPath 顶层的机器本地工件
// （TUI 快照、同步状态、锁）不会被当成 config 条目收集，且其内容变化不
// 产生待推送条目。
func TestCollectSkipsMachineLocalArtifacts(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	writeEnvVar(t, cache, "default", "A", "aaa")

	st, err := cache.loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}

	artifacts := []string{"tui-snapshot.enc", ".senv-sync-state.json", ".senv-sync.lock"}
	writeAll := func(marker string) {
		for _, name := range artifacts {
			if err := os.WriteFile(filepath.Join(cache.dataPath, name), []byte(marker+"-"+name), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeAll("first")

	entries, err := cache.collect()
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, key := range []string{"tui-snapshot", ".senv-sync-state"} {
		if _, ok := entries[entryID(KindConfig, "", key)]; ok {
			t.Fatalf("machine-local artifact %q was collected as config entry", key)
		}
	}

	dirtyBefore := len(p.collectDirty(st, entries))
	writeAll("second") // 内容变化
	cache.resetCollectCache()
	entries2, err := cache.collect()
	if err != nil {
		t.Fatalf("collect after rewrite: %v", err)
	}
	dirtyAfter := len(p.collectDirty(st, entries2))
	if dirtyBefore != dirtyAfter {
		t.Fatalf("machine-local rewrite changed dirty count %d -> %d", dirtyBefore, dirtyAfter)
	}
}
