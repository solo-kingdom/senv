package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotCacheIgnoresTreesAndLedgerLogs(t *testing.T) {
	mgrs, cache, dataPath := snapshotTestVault(t)
	seedSnapshotData(t, mgrs)

	if !cache.Write(mgrs) {
		t.Fatal("Write failed")
	}

	// Writing unrelated trees/ and ledger_logs/ files must NOT invalidate SnapshotCache.
	for _, rel := range []string{
		filepath.Join("trees", "notes", "doc.enc"),
		filepath.Join("ledger_logs", "default", "srv.n1@20261126T000000Z-abcd.enc"),
	} {
		full := filepath.Join(dataPath, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("opaque"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if payload := cache.TryLoad(); payload == nil {
		t.Fatalf("SnapshotCache invalidated by trees/ or ledger_logs/ write")
	}

	// Writing an env file MUST invalidate SnapshotCache.
	if err := mgrs.Env.Set("default", "NEW_KEY", "new-val"); err != nil {
		t.Fatal(err)
	}
	if payload := cache.TryLoad(); payload != nil {
		t.Fatalf("SnapshotCache did not invalidate after env write")
	}
}
