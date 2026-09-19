package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeBackupCache(t *testing.T, cache *localCache, kind, grp, key, content string) {
	t.Helper()
	path := mustEntryPath(t, cache, kind, grp, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCollectBackupPushDirection(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)
	writeBackupCache(t, cache, KindBackup, "notes", "DUMP", "cipher-dump")
	writeBackupCache(t, cache, KindBackupMeta, "notes", "", "cipher-meta")

	entries, err := cache.collectEntries()
	if err != nil {
		t.Fatalf("collectEntries: %v", err)
	}
	dumpID := entryID(KindBackup, "notes", "DUMP")
	metaID := entryID(KindBackupMeta, "notes", "")
	if got := string(entries[dumpID].Ciphertext); got != "cipher-dump" {
		t.Fatalf("backup ciphertext = %q", got)
	}
	if got := string(entries[metaID].Ciphertext); got != "cipher-meta" {
		t.Fatalf("backup meta ciphertext = %q", got)
	}
}

func TestCollectBackupMissingDirIsSilent(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)
	if _, err := cache.collectEntries(); err != nil {
		t.Fatalf("collect without backups/ = %v", err)
	}
}

func TestCollectBackupIncrementalMatchesFull(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)
	writeBackupCache(t, cache, KindBackup, "notes", "SEED", "seed")
	if _, err := cache.collect(); err != nil {
		t.Fatalf("seed collect: %v", err)
	}

	writeBackupCache(t, cache, KindBackup, "notes", "DUMP", "changed")
	writeBackupCache(t, cache, KindBackupMeta, "notes", "", "meta")

	incremental, reads, err := cache.collectCached()
	if err != nil {
		t.Fatalf("incremental collect: %v", err)
	}
	if reads != 2 {
		t.Fatalf("reads = %d, want 2 (changed + new meta)", reads)
	}
	cache.resetCollectCache()
	full, err := cache.collect()
	if err != nil {
		t.Fatalf("full collect: %v", err)
	}
	if !collectMapsEqual(incremental, full) {
		t.Fatal("incremental collect with backup writes diverged from a full rescan")
	}
}

func TestPullAppliesBackup(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()

	if _, _, err := srv.Push(ctx, "main", []Entry{
		{Kind: KindBackup, Grp: "notes", Key: "DUMP", Ciphertext: []byte("dump-blob"), BaseRevision: 0},
		{Kind: KindBackupMeta, Grp: "notes", Ciphertext: []byte("meta-blob"), BaseRevision: 0},
	}); err != nil {
		t.Fatalf("seed push: %v", err)
	}

	res, err := p.SyncWithReport(ctx)
	if err != nil {
		t.Fatalf("SyncWithReport: %v", err)
	}
	if res.Pull.Applied != 2 {
		t.Fatalf("pull applied = %d, want 2", res.Pull.Applied)
	}
	for _, tc := range []struct {
		kind, grp, key, want string
	}{
		{KindBackup, "notes", "DUMP", "dump-blob"},
		{KindBackupMeta, "notes", "", "meta-blob"},
	} {
		path := mustEntryPath(t, cache, tc.kind, tc.grp, tc.key)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("pull did not land %s: %v", tc.kind, err)
		}
		if string(data) != tc.want {
			t.Fatalf("%s content = %q, want %q", tc.kind, data, tc.want)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s perm = %v, want 0600", tc.kind, info.Mode().Perm())
		}
	}

	res, err = p.SyncWithReport(ctx)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if res.Dirty != 0 || res.Pull.Applied != 0 || res.Push.Pushed != 0 {
		t.Errorf("converged sync should be no-op: %+v", res)
	}
}
