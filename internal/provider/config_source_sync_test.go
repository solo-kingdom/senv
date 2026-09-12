package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeProfile 在缓存数据目录写入一个配置源档案 blob（LLM Provider / MCP Server）
func writeProfile(t *testing.T, cache *localCache, kind, alias, content string) {
	t.Helper()
	path := mustEntryPath(t, cache, kind, "", alias)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCollectProfilesPushDirection(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)
	writeProfile(t, cache, KindLLMProvider, "anthropic", `{"alias":"anthropic"}`)
	writeProfile(t, cache, KindMCPServer, "github-mcp", `{"alias":"github-mcp"}`)

	entries, err := cache.collectEntries()
	if err != nil {
		t.Fatalf("collectEntries: %v", err)
	}
	for _, id := range []string{
		entryID(KindLLMProvider, "", "anthropic"),
		entryID(KindMCPServer, "", "github-mcp"),
	} {
		e, ok := entries[id]
		if !ok {
			t.Fatalf("missing collected entry %q", id)
		}
		if e.Grp != "" {
			t.Errorf("entry %q grp = %q, want empty", id, e.Grp)
		}
	}
	if got := string(entries[entryID(KindLLMProvider, "", "anthropic")].Ciphertext); got != `{"alias":"anthropic"}` {
		t.Fatalf("llm provider ciphertext = %q", got)
	}
}

func TestCollectProfilesIncrementalMatchesFull(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)
	writeProfile(t, cache, KindLLMProvider, "seed", "seed")
	if _, err := cache.collect(); err != nil {
		t.Fatalf("seed collect: %v", err)
	}

	writeProfile(t, cache, KindLLMProvider, "anthropic", "changed")
	writeProfile(t, cache, KindMCPServer, "github-mcp", "new")

	incremental, reads, err := cache.collectCached()
	if err != nil {
		t.Fatalf("incremental collect: %v", err)
	}
	if reads != 2 {
		t.Fatalf("reads = %d, want 2 (changed + new)", reads)
	}
	cache.resetCollectCache()
	full, err := cache.collect()
	if err != nil {
		t.Fatalf("full collect: %v", err)
	}
	if !collectMapsEqual(incremental, full) {
		t.Fatal("incremental collect with profile writes diverged from a full rescan")
	}
}

func TestCollectProfilesIgnoresForeignFiles(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)
	writeProfile(t, cache, KindMCPServer, "valid", "x")
	llmDir := filepath.Dir(mustEntryPath(t, cache, KindLLMProvider, "", "x"))
	if err := os.MkdirAll(filepath.Join(llmDir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(llmDir, "notes.txt"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(llmDir, "nested", "deep.enc"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := cache.collectEntries()
	if err != nil {
		t.Fatalf("collectEntries: %v", err)
	}
	if _, ok := entries[entryID(KindLLMProvider, "", "notes")]; ok {
		t.Error("non-.enc file was collected as a profile")
	}
	if _, ok := entries[entryID(KindLLMProvider, "", "nested")]; ok {
		t.Error("directory was collected as a profile")
	}
	if _, ok := entries[entryID(KindMCPServer, "", "valid")]; !ok {
		t.Error("valid profile missing from collection")
	}
}

func TestPullAppliesProfiles(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()

	srv.Push(ctx, "main", []Entry{
		{Kind: KindLLMProvider, Key: "anthropic", Ciphertext: []byte("llm-blob"), BaseRevision: 0},
		{Kind: KindMCPServer, Key: "github-mcp", Ciphertext: []byte("mcp-blob"), BaseRevision: 0},
	})

	res, err := p.SyncWithReport(ctx)
	if err != nil {
		t.Fatalf("SyncWithReport: %v", err)
	}
	if res.Pull.Applied != 2 {
		t.Fatalf("pull applied = %d, want 2", res.Pull.Applied)
	}
	for _, tc := range []struct {
		kind, alias, want string
	}{
		{KindLLMProvider, "anthropic", "llm-blob"},
		{KindMCPServer, "github-mcp", "mcp-blob"},
	} {
		path := mustEntryPath(t, cache, tc.kind, "", tc.alias)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("pull did not land %s profile: %v", tc.kind, err)
		}
		if string(data) != tc.want {
			t.Fatalf("%s profile content = %q, want %q", tc.kind, data, tc.want)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s profile perm = %v, want 0600", tc.kind, info.Mode().Perm())
		}
	}

	// 收敛后再次同步应为 no-op
	res, err = p.SyncWithReport(ctx)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if res.Dirty != 0 || res.Pull.Applied != 0 || res.Push.Pushed != 0 {
		t.Errorf("converged sync should be no-op: %+v", res)
	}
}

// TestLocalSyncSnapshotLastPull：快照的 lastPull 反映 .senv-sync-state.json
// 的 LastPullAt；无状态文件（bootstrap 前）为零值。
func TestLocalSyncSnapshotLastPull(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)

	// newTestProvider 的 Bootstrap 即首次 pull：LastPullAt 已持久化
	dirty, lastPull, err := p.LocalSyncSnapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	_ = dirty
	if lastPull.IsZero() {
		t.Fatal("lastPull after bootstrap = zero, want set")
	}

	// 快照值与状态文件的 LastPullAt 一致
	st, err := cache.loadState()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if st.LastPullAt <= 0 {
		t.Fatalf("LastPullAt = %d, want > 0 after pull", st.LastPullAt)
	}
	if want := time.Unix(st.LastPullAt, 0); !lastPull.Equal(want) {
		t.Fatalf("lastPull = %v, want %v", lastPull, want)
	}

	// 状态文件里 LastPullAt=0（从未 pull 的 vault 快照）→ lastPull 零值。
	// 直接改写状态文件，绕开防退化校验（本测试只关心快照读路径）。
	st.LastPullAt = 0
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache.stateFilePath(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, lastPull, err = p.LocalSyncSnapshot(); err != nil {
		t.Fatalf("snapshot with zero LastPullAt: %v", err)
	} else if !lastPull.IsZero() {
		t.Fatalf("lastPull with zero LastPullAt = %v, want zero", lastPull)
	}
}
