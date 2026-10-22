package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func TestSkillEntryLocation(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)

	loc, err := cache.entryLocation(KindSkill, "work", "demo")
	if err != nil {
		t.Fatalf("entryLocation skill: %v", err)
	}
	want := []string{storage.SkillDirName, "work", "demo" + storage.SkillFileSuffix}
	if loc.root != cacheDataRoot || filepath.Join(loc.segments...) != filepath.Join(want...) {
		t.Fatalf("skill location = %+v, want data root %v", loc, want)
	}

	loc, err = cache.entryLocation(KindSkillIndex, "", "")
	if err != nil {
		t.Fatalf("entryLocation skill_index: %v", err)
	}
	if loc.root != cacheDataRoot || len(loc.segments) != 1 || loc.segments[0] != storage.SkillIndexFile {
		t.Fatalf("skill_index must live in the data dir, got %+v", loc)
	}

	for _, bad := range [][3]string{
		{KindSkill, "", "demo"},
		{KindSkill, "..", "demo"},
		{KindSkill, "work", "../demo"},
		{KindSkillIndex, "work", ""},
	} {
		if _, err := cache.entryLocation(bad[0], bad[1], bad[2]); err == nil {
			t.Errorf("entryLocation(%v) should be rejected", bad)
		}
	}
}

func TestCollectSkillPushDirection(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)
	writeBackupCache(t, cache, KindSkill, "work", "demo", "cipher-skill")
	writeBackupCache(t, cache, KindSkillIndex, "", "", `{"skills":{}}`)

	entries, err := cache.collectEntries()
	if err != nil {
		t.Fatalf("collectEntries: %v", err)
	}
	if got := string(entries[entryID(KindSkill, "work", "demo")].Ciphertext); got != "cipher-skill" {
		t.Fatalf("skill ciphertext = %q", got)
	}
	if got := string(entries[entryID(KindSkillIndex, "", "")].Ciphertext); got != `{"skills":{}}` {
		t.Fatalf("skill_index = %q", got)
	}
	// skill_index.json 不能被当成 config 条目
	for _, e := range entries {
		if e.Kind == KindConfig {
			t.Fatalf("unexpected config entry from skill files: %+v", e)
		}
	}
}

func TestCollectSkillMissingDirIsSilent(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)
	if _, err := cache.collectEntries(); err != nil {
		t.Fatalf("collect without skills/ = %v", err)
	}
}

func TestCollectSkillIgnoresNonEncAndRejectsStrayDirs(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)
	writeBackupCache(t, cache, KindSkill, "work", "demo", "cipher")
	stray := filepath.Join(cache.dataPath, storage.SkillDirName, "work", "demo.enc.rekey-x.new")
	if err := os.WriteFile(stray, []byte("sidecar"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := cache.collectEntries()
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want only the .enc skill", len(entries))
	}

	if err := os.Mkdir(filepath.Join(cache.dataPath, storage.SkillDirName, "work", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.collectEntries(); err == nil {
		t.Fatal("subdirectory inside a skill group must fail closed")
	}
}

func TestCollectSkillIncrementalMatchesFull(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)
	writeBackupCache(t, cache, KindSkill, "work", "seed", "seed")
	if _, err := cache.collect(); err != nil {
		t.Fatalf("seed collect: %v", err)
	}
	writeBackupCache(t, cache, KindSkill, "work", "demo", "changed")
	writeBackupCache(t, cache, KindSkillIndex, "", "", `{"skills":{}}`)

	incremental, reads, err := cache.collectCached()
	if err != nil {
		t.Fatalf("incremental collect: %v", err)
	}
	if reads != 2 {
		t.Fatalf("reads = %d, want 2 (new skill + index)", reads)
	}
	cache.resetCollectCache()
	full, err := cache.collect()
	if err != nil {
		t.Fatalf("full collect: %v", err)
	}
	if !collectMapsEqual(incremental, full) {
		t.Fatal("incremental skill collect diverged from a full rescan")
	}
}

func TestPullAppliesSkill(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()

	if _, _, err := srv.Push(ctx, "main", []Entry{
		{Kind: KindSkill, Grp: "work", Key: "demo", Ciphertext: []byte("skill-blob"), BaseRevision: 0},
		{Kind: KindSkillIndex, Ciphertext: []byte(`{"skills":{}}`), BaseRevision: 0},
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
	for _, tc := range []struct{ kind, grp, key, want string }{
		{KindSkill, "work", "demo", "skill-blob"},
		{KindSkillIndex, "", "", `{"skills":{}}`},
	} {
		path := mustEntryPath(t, cache, tc.kind, tc.grp, tc.key)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("pull did not land %s: %v", tc.kind, err)
		}
		if string(data) != tc.want {
			t.Fatalf("%s content = %q, want %q", tc.kind, data, tc.want)
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s perm = %v, want 0600", tc.kind, info.Mode().Perm())
		}
	}
	// 落点在数据目录内
	if _, err := os.Stat(filepath.Join(cache.dataPath, storage.SkillDirName, "work", "demo.enc")); err != nil {
		t.Fatalf("skill not under data dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache.dataPath, storage.SkillIndexFile)); err != nil {
		t.Fatalf("skill index not under data dir: %v", err)
	}

	res, err = p.SyncWithReport(ctx)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if res.Dirty != 0 || res.Pull.Applied != 0 || res.Push.Pushed != 0 {
		t.Errorf("converged sync should be no-op: %+v", res)
	}
}

func TestPullRejectsTraversalSkillIdentity(t *testing.T) {
	srv := newFakeServer()
	_, cache := newTestProvider(t, srv)
	if err := validateRemoteEntries([]Entry{{Kind: KindSkill, Grp: "work", Key: "../escape"}}); err == nil {
		t.Fatal("remote skill identity with traversal must be rejected")
	}
	if err := cache.apply(Entry{Kind: KindSkill, Grp: "..", Key: "x", Ciphertext: []byte("x")}); err == nil {
		t.Fatal("apply must reject a traversal group")
	}
}
