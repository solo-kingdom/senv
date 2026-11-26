package storage

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/crypto"
)

func TestSkillIndexMissingFileIsEmpty(t *testing.T) {
	mgr, _ := setupTestManager(t)
	index, err := mgr.LoadSkillIndex()
	if err != nil {
		t.Fatalf("LoadSkillIndex: %v", err)
	}
	if len(index.Skills) != 0 {
		t.Fatalf("fresh vault skills = %v, want none", index.Skills)
	}
}

func TestSkillIndexLivesInDataDir(t *testing.T) {
	mgr, _ := setupTestManager(t)
	now := time.Now()
	index := NewSkillIndex()
	index.Skills["demo"] = SkillEntry{ID: "demo", Description: "d", CreatedAt: now, UpdatedAt: now}
	if err := mgr.SaveSkillIndex(index); err != nil {
		t.Fatalf("SaveSkillIndex: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mgr.GetDataPath(), SkillIndexFile)); err != nil {
		t.Fatalf("skill index not in data dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mgr.GetConfigPath(), SkillIndexFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("skill index must not be written to config dir, stat err = %v", err)
	}
	loaded, err := mgr.LoadSkillIndex()
	if err != nil {
		t.Fatalf("LoadSkillIndex: %v", err)
	}
	got := loaded.Skills["demo"]
	if got.Group != SkillDefaultGroup || got.EncryptedFile != "skills/default/demo.enc" || got.Description != "d" {
		t.Fatalf("normalized entry = %+v", got)
	}
}

func TestSkillIndexRejectsBadIdentities(t *testing.T) {
	mgr, _ := setupTestManager(t)
	for name, entry := range map[string]SkillEntry{
		"id mismatch":   {ID: "other"},
		"bad group":     {ID: "x", Group: "../g"},
		"bad enc path":  {ID: "x", EncryptedFile: "skills/other/x.enc"},
		"escaping path": {ID: "x", EncryptedFile: "../x.enc"},
	} {
		t.Run(name, func(t *testing.T) {
			index := NewSkillIndex()
			index.Skills["x"] = entry
			if err := mgr.SaveSkillIndex(index); err == nil {
				t.Fatal("SaveSkillIndex should reject the entry")
			}
		})
	}
	index := NewSkillIndex()
	index.Skills["../x"] = SkillEntry{ID: "../x"}
	if err := mgr.SaveSkillIndex(index); err == nil {
		t.Fatal("SaveSkillIndex should reject a traversal id")
	}

	// 磁盘上被篡改/同步来的坏索引：读取失败关闭。
	bad := `{"skills":{"x":{"id":"y","encrypted_file":"skills/default/y.enc"}}}`
	if err := os.WriteFile(filepath.Join(mgr.GetDataPath(), SkillIndexFile), []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.LoadSkillIndex(); err == nil {
		t.Fatal("LoadSkillIndex should fail closed on identity mismatch")
	}
}

func TestSkillSaveLoadRoundTripAndEncryption(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	archive := []byte("pretend-tar-bytes-SECRET-MARKER")

	if err := mgr.SaveSkillWithKey("work", "demo", archive, key); err != nil {
		t.Fatalf("SaveSkillWithKey: %v", err)
	}
	path := filepath.Join(mgr.GetDataPath(), SkillDirName, "work", "demo.enc")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ciphertext file: %v", err)
	}
	if bytes.Contains(raw, []byte("SECRET-MARKER")) {
		t.Fatal("skill ciphertext leaks plaintext")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v, want 0600", info.Mode().Perm())
	}
	got, err := mgr.LoadSkillWithKey("work", "demo", key)
	if err != nil {
		t.Fatalf("LoadSkillWithKey: %v", err)
	}
	if !bytes.Equal(got, archive) {
		t.Fatal("round trip mismatch")
	}

	if err := mgr.DeleteSkill("work", "demo"); err != nil {
		t.Fatalf("DeleteSkill: %v", err)
	}
	if _, err := mgr.LoadSkillWithKey("work", "demo", key); err == nil {
		t.Fatal("deleted skill should not load")
	}
	// 重复删除幂等
	if err := mgr.DeleteSkill("work", "demo"); err != nil {
		t.Fatalf("second DeleteSkill: %v", err)
	}
}

func TestSkillPlaintextLimit(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")

	if err := mgr.SaveSkillWithKey("default", "max", make([]byte, MaxSkillPlaintext), key); err != nil {
		t.Fatalf("1 MiB exactly should be accepted: %v", err)
	}
	err := mgr.SaveSkillWithKey("default", "big", make([]byte, MaxSkillPlaintext+1), key)
	if err == nil {
		t.Fatal("over 1 MiB should be rejected")
	}
	if !strings.Contains(err.Error(), "1 MiB") || !strings.Contains(err.Error(), "1048576") {
		t.Fatalf("error should name the limit: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(mgr.GetDataPath(), SkillDirName, "default", "big.enc")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("rejected skill must not be written")
	}
}

// 1 MiB 明文加密后的密文必须落在 server 的 1.5MiB 上限内。
func TestSkillMaxCiphertextFitsServerLimit(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	if err := mgr.SaveSkillWithKey("default", "max", make([]byte, MaxSkillPlaintext), key); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(mgr.GetDataPath(), SkillDirName, "default", "max.enc"))
	if err != nil {
		t.Fatal(err)
	}
	const serverSkillLimit = 1536 * 1024
	if len(raw) > serverSkillLimit {
		t.Fatalf("1 MiB plaintext encrypts to %d bytes, over server limit %d", len(raw), serverSkillLimit)
	}
}

func TestSkillIdentityValidation(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	for _, bad := range []string{"", "..", "a/b", `a\b`, "a:b"} {
		if err := mgr.SaveSkillWithKey("default", bad, []byte("x"), key); err == nil {
			t.Errorf("id %q should be rejected", bad)
		}
		if err := mgr.SaveSkillWithKey(bad, "ok", []byte("x"), key); err == nil {
			t.Errorf("group %q should be rejected", bad)
		}
	}
}

func TestSkillSaveRejectsStaleKey(t *testing.T) {
	mgr, _ := setupTestManager(t)
	otherSalt, _ := crypto.GenerateSalt()
	otherKey := crypto.DeriveKey("other", otherSalt)
	if err := mgr.SaveSkillWithKey("default", "x", []byte("x"), otherKey); !errors.Is(err, ErrDataDesync) {
		t.Fatalf("error = %v, want ErrDataDesync", err)
	}
}

func TestListSkillFiles(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	if refs, err := mgr.ListSkillFiles(); err != nil || len(refs) != 0 {
		t.Fatalf("empty vault refs=%v err=%v", refs, err)
	}
	for _, p := range [][2]string{{"b", "two"}, {"a", "one"}, {"a", "zed"}} {
		if err := mgr.SaveSkillWithKey(p[0], p[1], []byte("x"), key); err != nil {
			t.Fatal(err)
		}
	}
	// 分组目录里的非 .enc 普通文件（如 rekey 旁路文件）被忽略
	if err := os.WriteFile(filepath.Join(mgr.GetDataPath(), SkillDirName, "a", "one.enc.rekey-abc.new"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	refs, err := mgr.ListSkillFiles()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range refs {
		got = append(got, r.Group+"/"+r.ID)
	}
	if strings.Join(got, ",") != "a/one,a/zed,b/two" {
		t.Fatalf("refs = %v", got)
	}

	// 非法历史身份失败关闭
	if err := os.WriteFile(filepath.Join(mgr.GetDataPath(), SkillDirName, "a", "bad:name.enc"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ListSkillFiles(); err == nil {
		t.Fatal("invalid historical identity should fail closed")
	}
}

func TestCheckConsistencyProbesSkills(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")
	if err := mgr.SaveSkillWithKey("default", "good", []byte("x"), key); err != nil {
		t.Fatal(err)
	}
	report, err := mgr.CheckConsistency(key)
	if err != nil {
		t.Fatal(err)
	}
	if report.SkillFiles.Total != 1 || report.SkillFiles.OK != 1 || !report.AllOK() {
		t.Fatalf("report = %+v", report.SkillFiles)
	}

	// 用外来 key 加密的密文被指认出来，AllOK 为 false
	otherSalt, _ := crypto.GenerateSalt()
	otherKey := crypto.DeriveKey("other", otherSalt)
	enc, _ := crypto.Encrypt(otherKey, []byte("x"))
	if err := os.WriteFile(filepath.Join(mgr.GetDataPath(), SkillDirName, "default", "rogue.enc"), []byte(enc), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err = mgr.CheckConsistency(key)
	if err != nil {
		t.Fatal(err)
	}
	if report.SkillFiles.Total != 2 || report.SkillFiles.OK != 1 || report.AllOK() {
		t.Fatalf("report = %+v", report.SkillFiles)
	}
	if len(report.SkillFiles.Failed) != 1 || !strings.Contains(report.SkillFiles.Failed[0], "rogue.enc") {
		t.Fatalf("failed = %v", report.SkillFiles.Failed)
	}
}

// 丢失 metadata 与 index、只剩 skills/ 密文时，init 必须拒绝，避免铸新 key 使密文永久不可解。
func TestInitGuardRefusesWhenOrphanedSkillExists(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "cfg")
	data := filepath.Join(tmp, "data")
	group := filepath.Join(data, SkillDirName, "default")
	if err := os.MkdirAll(group, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(group, "demo"+SkillFileSuffix), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(cfg, data)
	if !mgr.HasOrphanedData() {
		t.Fatal("HasOrphanedData should see skills/ ciphertext")
	}
	if err := mgr.Initialize("pw"); !errors.Is(err, ErrOrphanedData) {
		t.Fatalf("Initialize error = %v, want ErrOrphanedData", err)
	}
	if _, err := os.Stat(filepath.Join(cfg, MetadataFile)); err == nil {
		t.Fatal("Initialize must not create metadata when refusing")
	}
}

func TestInitGuardIgnoresEmptySkillsDirAndIndexOnly(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "cfg")
	data := filepath.Join(tmp, "data")
	if err := os.MkdirAll(filepath.Join(data, SkillDirName, "default"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, SkillIndexFile), []byte(`{"skills":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(cfg, data)
	if mgr.HasOrphanedData() {
		t.Fatal("empty skills dir and plaintext index alone are not orphaned ciphertext")
	}
}

func TestRekeyReEncryptsSkills(t *testing.T) {
	mgr, _ := setupTestManager(t)
	oldKey := derivedKey(t, mgr, "test-password")
	archive := []byte("tar-bytes")
	if err := mgr.SaveSkillWithKey("work", "demo", archive, oldKey); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	index := NewSkillIndex()
	index.Skills["demo"] = SkillEntry{ID: "demo", Group: "work", CreatedAt: now, UpdatedAt: now}
	if err := mgr.SaveSkillIndexWithKey(index, oldKey); err != nil {
		t.Fatal(err)
	}
	indexBefore, _ := os.ReadFile(filepath.Join(mgr.GetDataPath(), SkillIndexFile))

	newSalt, _ := crypto.GenerateSalt()
	newKey := crypto.DeriveKeyWithIterations("new-password", newSalt, crypto.DefaultIterations)
	newPasswordKey, _ := crypto.EncryptWithAAD(newKey, []byte(crypto.HashPassword("new-password")), metadataVerifierAAD(2))
	result, err := mgr.Rekey(oldKey, newKey, base64.StdEncoding.EncodeToString(newSalt), newPasswordKey, crypto.DefaultIterations)
	if err != nil {
		t.Fatalf("Rekey: %v", err)
	}
	if result.SkillFiles != 1 {
		t.Fatalf("SkillFiles = %d, want 1", result.SkillFiles)
	}
	if _, err := mgr.LoadSkillWithKey("work", "demo", oldKey); err == nil {
		t.Fatal("old key must not decrypt after rekey")
	}
	got, err := mgr.LoadSkillWithKey("work", "demo", newKey)
	if err != nil || !bytes.Equal(got, archive) {
		t.Fatalf("new key load: %v %q", err, got)
	}
	indexAfter, _ := os.ReadFile(filepath.Join(mgr.GetDataPath(), SkillIndexFile))
	if !crypto.IsV2Ciphertext(string(indexAfter)) || bytes.Equal(indexBefore, indexAfter) {
		t.Fatal("skill index must be re-encrypted with v2 envelope on rekey")
	}
	if _, err := mgr.LoadSkillIndexWithKey(oldKey); err == nil {
		t.Fatal("old key must not decrypt skill index after rekey")
	}
	loadedIdx, err := mgr.LoadSkillIndexWithKey(newKey)
	if err != nil || len(loadedIdx.Skills) != 1 {
		t.Fatalf("LoadSkillIndexWithKey(newKey): err=%v idx=%+v", err, loadedIdx)
	}
	report, err := mgr.CheckConsistency(newKey)
	if err != nil || !report.AllOK() {
		t.Fatalf("consistency after rekey: err=%v report=%+v", err, report)
	}
}

// 数据目录里 skills/ 下的坏身份会让 rekey 预检失败关闭，而不是被漏掉。
func TestRekeyRejectsInvalidSkillIdentity(t *testing.T) {
	mgr, _ := setupTestManager(t)
	oldKey := derivedKey(t, mgr, "test-password")
	dir := filepath.Join(mgr.GetDataPath(), SkillDirName, "default")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	enc, _ := crypto.Encrypt(oldKey, []byte("x"))
	if err := os.WriteFile(filepath.Join(dir, "bad:id.enc"), []byte(enc), 0o600); err != nil {
		t.Fatal(err)
	}
	newSalt, _ := crypto.GenerateSalt()
	newKey := crypto.DeriveKeyWithIterations("new-password", newSalt, crypto.DefaultIterations)
	newPasswordKey, _ := crypto.Encrypt(newKey, []byte(crypto.HashPassword("new-password")))
	if _, err := mgr.Rekey(oldKey, newKey, base64.StdEncoding.EncodeToString(newSalt), newPasswordKey, crypto.DefaultIterations); err == nil {
		t.Fatal("Rekey should fail closed on an invalid skill identity")
	}
}

func TestClassifyRekeyEntrySkill(t *testing.T) {
	kind, err := classifyRekeyEntry([]string{SkillDirName, "default", "demo.enc"}, map[string]bool{})
	if err != nil || kind != rekeyEntrySkill {
		t.Fatalf("kind=%v err=%v", kind, err)
	}
	for _, segs := range [][]string{
		{SkillDirName, "default", "demo"},    // 无 .enc 后缀
		{SkillDirName, "..", "demo.enc"},     // 坏分组
		{SkillDirName, "default", ".enc"},    // 空 id
		{SkillDirName, "demo.enc"},           // 层级不对
		{SkillDirName, "a", "b", "demo.enc"}, // 层级不对
	} {
		if _, err := classifyRekeyEntry(segs, map[string]bool{}); err == nil {
			t.Errorf("segments %v should be rejected", segs)
		}
	}
}

func TestSkillIndexGroupsAndEffectivePolicy(t *testing.T) {
	mgr, _ := setupTestManager(t)
	now := time.Now()
	yes, no := true, false

	index := NewSkillIndex()
	index.Groups["base"] = SkillGroup{
		Name:           "base",
		Description:    "global base skills",
		DefaultInstall: &yes,
		InstallScope:   SkillScopeGlobal,
		Targets:        []string{"agents"},
	}
	index.Groups["proj"] = SkillGroup{
		Name:           "proj",
		Description:    "project skills",
		DefaultInstall: &yes,
		InstallScope:   SkillScopeProject,
		InstallPath:    "~/code/",
	}
	index.Skills["a"] = SkillEntry{ID: "a", Group: "base", CreatedAt: now, UpdatedAt: now}
	index.Skills["b"] = SkillEntry{ID: "b", Group: "base", DefaultInstall: &no, Targets: []string{"claude"}, CreatedAt: now, UpdatedAt: now}
	index.Skills["c"] = SkillEntry{ID: "c", Group: "proj", CreatedAt: now, UpdatedAt: now}
	index.Skills["d"] = SkillEntry{ID: "d", Group: "legacy", CreatedAt: now, UpdatedAt: now}

	if err := mgr.SaveSkillIndex(index); err != nil {
		t.Fatalf("SaveSkillIndex: %v", err)
	}
	loaded, err := mgr.LoadSkillIndex()
	if err != nil {
		t.Fatalf("LoadSkillIndex: %v", err)
	}
	if loaded.Groups["proj"].InstallPath != "~/code" {
		t.Fatalf("normalized install_path = %q, want ~/code", loaded.Groups["proj"].InstallPath)
	}

	pa := loaded.EffectivePolicy(loaded.Skills["a"])
	if !pa.DefaultInstall || !pa.DefaultInstallInherited || pa.InstallScope != SkillScopeGlobal || len(pa.Targets) != 1 || pa.Targets[0] != "agents" {
		t.Fatalf("policy a = %+v", pa)
	}
	pb := loaded.EffectivePolicy(loaded.Skills["b"])
	if pb.DefaultInstall || pb.DefaultInstallInherited || len(pb.Targets) != 1 || pb.Targets[0] != "claude" {
		t.Fatalf("policy b = %+v", pb)
	}
	pc := loaded.EffectivePolicy(loaded.Skills["c"])
	if !pc.DefaultInstall || pc.InstallScope != SkillScopeProject || pc.InstallPath != "~/code" || !pc.InstallPathInherited {
		t.Fatalf("policy c = %+v", pc)
	}
	pd := loaded.EffectivePolicy(loaded.Skills["d"])
	if pd.DefaultInstall || pd.InstallScope != SkillScopeGlobal || len(pd.Targets) != 2 {
		t.Fatalf("legacy policy d = %+v, want default=false scope=global targets=2", pd)
	}
}

func TestSkillIndexInstallMetaValidation(t *testing.T) {
	mgr, _ := setupTestManager(t)
	now := time.Now()
	badCases := []struct {
		name  string
		index *SkillIndex
	}{
		{
			name: "bad scope",
			index: &SkillIndex{Skills: map[string]SkillEntry{
				"x": {ID: "x", InstallScope: "workspace", CreatedAt: now, UpdatedAt: now},
			}},
		},
		{
			name: "relative install path",
			index: &SkillIndex{Skills: map[string]SkillEntry{
				"x": {ID: "x", InstallScope: SkillScopeProject, InstallPath: "rel/dir", CreatedAt: now, UpdatedAt: now},
			}},
		},
		{
			name: "traversal install path",
			index: &SkillIndex{Skills: map[string]SkillEntry{
				"x": {ID: "x", InstallScope: SkillScopeProject, InstallPath: "~/../etc", CreatedAt: now, UpdatedAt: now},
			}},
		},
		{
			name: "global with install path",
			index: &SkillIndex{Skills: map[string]SkillEntry{
				"x": {ID: "x", InstallScope: SkillScopeGlobal, InstallPath: "~/code", CreatedAt: now, UpdatedAt: now},
			}},
		},
		{
			name: "bad target",
			index: &SkillIndex{Skills: map[string]SkillEntry{
				"x": {ID: "x", Targets: []string{"kiro"}, CreatedAt: now, UpdatedAt: now},
			}},
		},
	}
	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := mgr.SaveSkillIndex(tc.index); err == nil {
				t.Fatalf("SaveSkillIndex should reject %s", tc.name)
			}
		})
	}
}

func TestSkillVersionValidationAndIndexNormalization(t *testing.T) {
	if err := ValidateSkillID("foo@v1"); err == nil {
		t.Fatal("ValidateSkillID must reject '@'")
	}
	if err := ValidateSkillStorageKey("foo@v1.0.0"); err != nil {
		t.Fatalf("ValidateSkillStorageKey(foo@v1.0.0): %v", err)
	}
	for _, bad := range []string{"@v1", "foo@", "foo@v1@v2", "foo@-bad", "foo@../x"} {
		if err := ValidateSkillStorageKey(bad); err == nil {
			t.Errorf("ValidateSkillStorageKey(%q) should fail", bad)
		}
	}
	if _, err := ValidateSkillVersionNote("ok note"); err != nil {
		t.Fatalf("valid note: %v", err)
	}
	if _, err := ValidateSkillVersionNote("line1\nline2"); err == nil {
		t.Fatal("multiline note must be rejected")
	}

	mgr, _ := setupTestManager(t)
	now := time.Now()
	validHex := strings.Repeat("a", 64)

	// 合法多版本索引 round-trip
	idx := NewSkillIndex()
	idx.Skills["demo"] = SkillEntry{
		ID:             "demo",
		CurrentVersion: "v2",
		Versions: []SkillVersion{
			{Version: "v1", SHA256: validHex, Size: 100, Note: "init", CreatedAt: now},
			{Version: "v2", SHA256: validHex, Size: 120, CreatedAt: now},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := mgr.SaveSkillIndex(idx); err != nil {
		t.Fatalf("SaveSkillIndex valid versioned: %v", err)
	}
	loaded, err := mgr.LoadSkillIndex()
	if err != nil {
		t.Fatalf("LoadSkillIndex: %v", err)
	}
	if loaded.Skills["demo"].CurrentVersion != "v2" || len(loaded.Skills["demo"].Versions) != 2 {
		t.Fatalf("loaded = %+v", loaded.Skills["demo"])
	}

	// fail-closed 损坏版本索引用例
	badVersions := []struct {
		name  string
		entry SkillEntry
	}{
		{"current_version without versions", SkillEntry{ID: "x", CurrentVersion: "v1"}},
		{"versions without current_version", SkillEntry{ID: "x", Versions: []SkillVersion{{Version: "v1", Size: 10}}}},
		{"current_version missing in versions", SkillEntry{ID: "x", CurrentVersion: "v2", Versions: []SkillVersion{{Version: "v1", Size: 10}}}},
		{"duplicate versions", SkillEntry{ID: "x", CurrentVersion: "v1", Versions: []SkillVersion{{Version: "v1", Size: 10}, {Version: "v1", Size: 10}}}},
		{"zero size", SkillEntry{ID: "x", CurrentVersion: "v1", Versions: []SkillVersion{{Version: "v1", Size: 0}}}},
		{"bad sha256", SkillEntry{ID: "x", CurrentVersion: "v1", Versions: []SkillVersion{{Version: "v1", Size: 10, SHA256: "xyz"}}}},
	}
	for _, tc := range badVersions {
		t.Run(tc.name, func(t *testing.T) {
			bad := NewSkillIndex()
			bad.Skills["x"] = tc.entry
			if err := mgr.SaveSkillIndex(bad); err == nil {
				t.Fatalf("SaveSkillIndex should reject %s", tc.name)
			}
		})
	}
}

func TestSkillVersionSaveLoadDeleteAndRekeyDoctor(t *testing.T) {
	mgr, _ := setupTestManager(t)
	key := derivedKey(t, mgr, "test-password")

	if err := mgr.SaveSkillWithKey("work", "demo", []byte("active-v2"), key); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveSkillVersionWithKey("work", "demo", "v1", []byte("snap-v1"), key); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveSkillVersionWithKey("work", "demo", "v2", []byte("active-v2"), key); err != nil {
		t.Fatal(err)
	}

	refs, err := mgr.ListSkillFiles()
	if err != nil {
		t.Fatalf("ListSkillFiles: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("ListSkillFiles = %+v, want 3 entries", refs)
	}
	gotV1, err := mgr.LoadSkillVersionWithKey("work", "demo", "v1", key)
	if err != nil || string(gotV1) != "snap-v1" {
		t.Fatalf("LoadSkillVersionWithKey(v1) = %q, %v", gotV1, err)
	}

	report, err := mgr.CheckConsistency(key)
	if err != nil || !report.AllOK() || report.SkillFiles.Total != 3 || report.SkillFiles.OK != 3 {
		t.Fatalf("CheckConsistency = %+v, err=%v", report, err)
	}

	newSalt, err := crypto.GenerateSalt()
	if err != nil {
		t.Fatal(err)
	}
	newKey := crypto.DeriveKeyWithIterations("new-password", newSalt, crypto.DefaultIterations)
	newPasswordKey, err := crypto.Encrypt(newKey, []byte(crypto.HashPassword("new-password")))
	if err != nil {
		t.Fatal(err)
	}
	rekeyRes, err := mgr.Rekey(key, newKey, base64.StdEncoding.EncodeToString(newSalt), newPasswordKey, crypto.DefaultIterations)
	if err != nil || rekeyRes.SkillFiles != 3 {
		t.Fatalf("Rekey = %+v, err=%v", rekeyRes, err)
	}
	gotV1After, err := mgr.LoadSkillVersionWithKey("work", "demo", "v1", newKey)
	if err != nil || string(gotV1After) != "snap-v1" {
		t.Fatalf("LoadSkillVersionWithKey after rekey = %q, %v", gotV1After, err)
	}

	if err := mgr.DeleteSkillVersion("work", "demo", "v1"); err != nil {
		t.Fatalf("DeleteSkillVersion: %v", err)
	}
	if _, err := mgr.LoadSkillVersionWithKey("work", "demo", "v1", newKey); err == nil {
		t.Fatal("deleted version snapshot should not load")
	}
}
