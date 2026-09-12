package provider

import (
	"context"
	"os"
	"testing"
)

// SSH 资产（hosts/keypairs）与 llm_provider/mcp_server 同构：别名身份、
// SSH-style 加密 blob、同一收集与落地通道（ADR-0020）。

func TestCollectSSHAssetsPushDirection(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)
	writeProfile(t, cache, KindSSHHost, "web-prod", `{"alias":"web-prod"}`)
	writeProfile(t, cache, KindSSHKeypair, "deploy-key", `{"name":"deploy-key","private_key":"PAID"}`)

	entries, err := cache.collectEntries()
	if err != nil {
		t.Fatalf("collectEntries: %v", err)
	}
	for _, id := range []string{
		entryID(KindSSHHost, "", "web-prod"),
		entryID(KindSSHKeypair, "", "deploy-key"),
	} {
		e, ok := entries[id]
		if !ok {
			t.Fatalf("missing collected entry %q", id)
		}
		if e.Grp != "" {
			t.Errorf("entry %q grp = %q, want empty", id, e.Grp)
		}
	}
	if got := string(entries[entryID(KindSSHKeypair, "", "deploy-key")].Ciphertext); got != `{"name":"deploy-key","private_key":"PAID"}` {
		t.Fatalf("ssh keypair ciphertext = %q", got)
	}
}

func TestCollectSSHAssetsIncrementalMatchesFull(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)
	writeProfile(t, cache, KindSSHHost, "seed", "seed")
	if _, err := cache.collect(); err != nil {
		t.Fatalf("seed collect: %v", err)
	}

	writeProfile(t, cache, KindSSHHost, "web-prod", "changed")
	writeProfile(t, cache, KindSSHKeypair, "deploy-key", "new")

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
		t.Fatal("incremental collect with ssh asset writes diverged from a full rescan")
	}
}

func TestPullAppliesSSHAssets(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()

	srv.Push(ctx, "main", []Entry{
		{Kind: KindSSHHost, Key: "web-prod", Ciphertext: []byte("host-blob"), BaseRevision: 0},
		{Kind: KindSSHKeypair, Key: "deploy-key", Ciphertext: []byte("keypair-blob"), BaseRevision: 0},
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
		{KindSSHHost, "web-prod", "host-blob"},
		{KindSSHKeypair, "deploy-key", "keypair-blob"},
	} {
		path := mustEntryPath(t, cache, tc.kind, "", tc.alias)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("pull did not land %s asset: %v", tc.kind, err)
		}
		if string(data) != tc.want {
			t.Fatalf("%s asset content = %q, want %q", tc.kind, data, tc.want)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s asset perm = %v, want 0600", tc.kind, info.Mode().Perm())
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
