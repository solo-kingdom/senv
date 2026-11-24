package provider

import (
	"context"
	"strings"
	"testing"
)

const (
	testMetaV600k = `{"version":"1.0","salt":"x","password_key":"y","kdf_iterations":600000}`
	testMetaV100k = `{"version":"1.0","salt":"x","password_key":"y","kdf_iterations":100000}`
	testMetaSwap  = `{"version":"1.0","salt":"swapped","password_key":"swapped","kdf_iterations":600000}`
)

func seededProvider(t *testing.T, meta string) (*ServerProvider, *fakeServer) {
	t.Helper()
	srv := newFakeServer()
	if _, _, err := srv.Push(context.Background(), "main", []Entry{{
		Kind: KindEnv, Grp: "default", Key: "K1", Ciphertext: []byte("ct-v1"),
	}}); err != nil {
		t.Fatalf("seed push: %v", err)
	}
	configPath := t.TempDir()
	dataPath := t.TempDir()
	srv.metadata["main"] = []byte(meta)
	p := newServerProvider(srv, configPath, dataPath, "main")
	if err := p.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	return p, srv
}

func TestPullRejectsRevisionRollbackAndTamper(t *testing.T) {
	p, srv := seededProvider(t, testMetaV600k)
	ctx := context.Background()
	id := entryID(KindEnv, "default", "K1")

	// 1. Vault-level latest revision rollback.
	st, err := p.cache.loadState()
	if err != nil {
		t.Fatal(err)
	}
	st.LastSyncedRevision = 10
	if err := p.cache.saveState(st); err != nil {
		t.Fatal(err)
	}
	if _, err := p.pull(ctx); err == nil || !strings.Contains(err.Error(), "回滚") {
		t.Fatalf("expected vault rollback error, got %v", err)
	}

	// 2. Entry-level revision rollback (local knows revision 5, server serves 1).
	// state hash 与本地文件一致，条目不是 dirty，才会进入回滚判定。
	cur, err := p.cache.collect()
	if err != nil {
		t.Fatal(err)
	}
	localHash := hashBytes(cur[id].Ciphertext)
	st, _ = p.cache.loadState()
	st.LastSyncedRevision = 0
	st.Entries[id] = syncEntryState{Revision: 5, Hash: localHash}
	if err := p.cache.saveState(st); err != nil {
		t.Fatal(err)
	}
	if _, err := p.pull(ctx); err == nil || !strings.Contains(err.Error(), "条目") || !strings.Contains(err.Error(), "回滚") {
		t.Fatalf("expected entry rollback error, got %v", err)
	}

	// 3. Same revision, server ciphertext differs from what local state recorded.
	rev := srv.entries["main"][id].Revision
	tampered := srv.entries["main"][id]
	tampered.Ciphertext = []byte("ct-tampered")
	srv.entries["main"][id] = tampered
	st, _ = p.cache.loadState()
	st.LastSyncedRevision = 0
	st.Entries[id] = syncEntryState{Revision: rev, Hash: localHash}
	if err := p.cache.saveState(st); err != nil {
		t.Fatal(err)
	}
	if _, err := p.pull(ctx); err == nil || !strings.Contains(err.Error(), "篡改") {
		t.Fatalf("expected same-revision tamper error, got %v", err)
	}
}

func TestPullRejectsKDFDowngrade(t *testing.T) {
	p, srv := seededProvider(t, testMetaV600k)
	srv.metadata["main"] = []byte(testMetaV100k)
	if _, err := p.pull(context.Background()); err == nil || !strings.Contains(err.Error(), "降级") {
		t.Fatalf("expected KDF downgrade rejection, got %v", err)
	}
}

func TestAutoPullDoesNotSilentlySwapSalt(t *testing.T) {
	p, srv := seededProvider(t, testMetaV600k)
	before, err := p.cache.readMetadata()
	if err != nil {
		t.Fatal(err)
	}
	srv.metadata["main"] = []byte(testMetaSwap)

	res, skip, err := p.AutoPull(context.Background(), 0, true)
	if err != nil {
		t.Fatalf("AutoPull: %v", err)
	}
	if skip != AutoSyncRan || res == nil || !res.MetadataConflict || res.MetadataUpdated {
		t.Fatalf("AutoPull = (skip=%q, res=%+v), want MetadataConflict only", skip, res)
	}
	after, _ := p.cache.readMetadata()
	if string(after) != string(before) {
		t.Fatalf("AutoPull silently overwrote local metadata: %q -> %q", before, after)
	}

	// 显式 pull（senv sync 路径）允许接受 salt 变更。
	res, err = p.pull(context.Background())
	if err != nil || !res.MetadataUpdated {
		t.Fatalf("explicit pull = (%+v, %v), want MetadataUpdated", res, err)
	}
}
