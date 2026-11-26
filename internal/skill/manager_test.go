package skill

import (
	"archive/tar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/storage"
)

func TestMain(m *testing.M) {
	crypto.LowerIterationsForTesting()
	os.Exit(m.Run())
}

func newTestManager(t *testing.T) (*Manager, *storage.Manager) {
	t.Helper()
	dir := t.TempDir()
	store := storage.NewManager(filepath.Join(dir, "config"), filepath.Join(dir, "data"))
	if err := store.Initialize("pw"); err != nil {
		t.Fatal(err)
	}
	return NewManager(store, "pw"), store
}

func skillSource(t *testing.T, name string, extra map[string]string) string {
	t.Helper()
	src := t.TempDir()
	files := map[string]string{"SKILL.md": "---\nname: " + name + "\ndescription: synthetic\n---\nbody-SECRET\n"}
	for k, v := range extra {
		files[k] = v
	}
	writeTree(t, src, files)
	return src
}

func TestManagerAddListShowRemove(t *testing.T) {
	mgr, store := newTestManager(t)
	src := skillSource(t, "alpha", map[string]string{"scripts/run.sh": "echo hi"})

	entry, err := mgr.Add(AddOptions{ID: "alpha", SourceDir: src, Description: "  first skill  "})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if entry.Group != "default" || entry.Description != "first skill" || entry.EncryptedFile != "skills/default/alpha.enc" {
		t.Fatalf("entry = %+v", entry)
	}
	// 数据目录里只有密文，不含明文
	raw, err := os.ReadFile(filepath.Join(store.GetDataPath(), "skills", "default", "alpha.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "SKILL.md") {
		t.Fatal("ciphertext leaks archive content")
	}

	list, err := mgr.List("")
	if err != nil || len(list) != 1 || list[0].ID != "alpha" {
		t.Fatalf("List = %+v err=%v", list, err)
	}
	if other, _ := mgr.List("nope"); len(other) != 0 {
		t.Fatalf("List(nope) = %+v", other)
	}

	detail, err := mgr.Show("alpha")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	paths := []string{}
	for _, e := range detail.Entries {
		paths = append(paths, e.Path)
	}
	if strings.Join(paths, ",") != "SKILL.md,scripts,scripts/run.sh" {
		t.Fatalf("show paths = %v", paths)
	}

	if err := mgr.Remove("alpha"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := mgr.Get("alpha"); err == nil {
		t.Fatal("removed skill must be gone from the index")
	}
	if _, err := os.Stat(filepath.Join(store.GetDataPath(), "skills", "default", "alpha.enc")); !os.IsNotExist(err) {
		t.Fatalf("ciphertext must be deleted, stat err = %v", err)
	}
	if err := mgr.Remove("alpha"); err == nil {
		t.Fatal("removing a missing skill must error")
	}
}

func TestManagerWithKey(t *testing.T) {
	_, store := newTestManager(t)
	key, err := store.DeriveKeyFromPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManagerWithKey(store, key)
	if _, err := mgr.Add(AddOptions{ID: "alpha", SourceDir: skillSource(t, "alpha", nil)}); err != nil {
		t.Fatalf("Add with key: %v", err)
	}
	if _, err := mgr.Show("alpha"); err != nil {
		t.Fatalf("Show with key: %v", err)
	}
}

func TestManagerAddRequiresFrontmatterNameEqualsID(t *testing.T) {
	mgr, store := newTestManager(t)
	_, err := mgr.Add(AddOptions{ID: "beta", SourceDir: skillSource(t, "not-beta", nil)})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("name mismatch error = %v", err)
	}
	noFM := t.TempDir()
	writeTree(t, noFM, map[string]string{"SKILL.md": "# no frontmatter\n"})
	if _, err := mgr.Add(AddOptions{ID: "beta", SourceDir: noFM}); err == nil {
		t.Fatal("missing frontmatter must be rejected")
	}
	// 拒绝后 vault 无任何残留
	if _, err := os.Stat(filepath.Join(store.GetDataPath(), "skills")); !os.IsNotExist(err) {
		t.Fatalf("rejected add must not create skills/, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.GetDataPath(), storage.SkillIndexFile)); !os.IsNotExist(err) {
		t.Fatalf("rejected add must not write the index, stat err = %v", err)
	}
}

func TestManagerRejectsBadIDsAndGroups(t *testing.T) {
	mgr, _ := newTestManager(t)
	src := skillSource(t, "alpha", nil)
	for _, id := range []string{"", "..", "a/b", "a:b", `a\b`} {
		if _, err := mgr.Add(AddOptions{ID: id, SourceDir: src}); err == nil {
			t.Errorf("id %q must be rejected", id)
		}
	}
	for _, group := range []string{"..", "a/b", "a:b"} {
		if _, err := mgr.Add(AddOptions{ID: "alpha", SourceDir: src, Group: group}); err == nil {
			t.Errorf("group %q must be rejected", group)
		}
	}
	if _, err := mgr.Add(AddOptions{ID: "alpha", SourceDir: src, Description: strings.Repeat("x", storage.MaxDescriptionBytes+1)}); err == nil {
		t.Error("over-long description must be rejected")
	}
}

func TestManagerIDsAreGloballyUniqueAcrossGroups(t *testing.T) {
	mgr, _ := newTestManager(t)
	src := skillSource(t, "alpha", nil)
	if _, err := mgr.Add(AddOptions{ID: "alpha", SourceDir: src, Group: "work"}); err != nil {
		t.Fatal(err)
	}
	_, err := mgr.Add(AddOptions{ID: "alpha", SourceDir: src, Group: "personal"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("same id in another group error = %v", err)
	}
	_, err = mgr.Add(AddOptions{ID: "alpha", SourceDir: src, Group: "work"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("same id in same group error = %v", err)
	}
	list, _ := mgr.List("")
	if len(list) != 1 || list[0].Group != "work" {
		t.Fatalf("list = %+v", list)
	}
}

func TestManagerForceReplaceKeepsCreatedAtAndGroup(t *testing.T) {
	mgr, store := newTestManager(t)
	first, err := mgr.Add(AddOptions{ID: "alpha", SourceDir: skillSource(t, "alpha", nil), Group: "work", Description: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := mgr.Add(AddOptions{ID: "alpha", SourceDir: skillSource(t, "alpha", map[string]string{"new.txt": "n"}), Force: true})
	if err != nil {
		t.Fatalf("force add: %v", err)
	}
	if second.Group != "work" || !second.CreatedAt.Equal(first.CreatedAt) || second.Description != "v1" {
		t.Fatalf("second = %+v first = %+v", second, first)
	}
	detail, err := mgr.Show("alpha")
	if err != nil || detail.Entry.Group != "work" {
		t.Fatalf("detail = %+v err=%v", detail, err)
	}
	found := false
	for _, e := range detail.Entries {
		found = found || e.Path == "new.txt"
	}
	if !found {
		t.Fatal("force replace must store the new content")
	}

	// 显式清空 description
	cleared, err := mgr.Add(AddOptions{ID: "alpha", SourceDir: skillSource(t, "alpha", nil), DescriptionSet: true, Description: "", Force: true})
	if err != nil {
		t.Fatalf("force clear description: %v", err)
	}
	if cleared.Description != "" {
		t.Fatalf("cleared description = %q, want empty", cleared.Description)
	}

	// 显式换组：旧分组密文被清理
	if _, err := mgr.Add(AddOptions{ID: "alpha", SourceDir: skillSource(t, "alpha", nil), Group: "personal", Force: true}); err != nil {
		t.Fatalf("force move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.GetDataPath(), "skills", "work", "alpha.enc")); !os.IsNotExist(err) {
		t.Fatalf("old group ciphertext must be removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.GetDataPath(), "skills", "personal", "alpha.enc")); err != nil {
		t.Fatalf("new group ciphertext missing: %v", err)
	}
}

func TestManagerListFiltersByGroupAndSorts(t *testing.T) {
	mgr, _ := newTestManager(t)
	for _, c := range []struct{ id, group string }{{"zeta", "a"}, {"alpha", "b"}, {"mid", "a"}} {
		if _, err := mgr.Add(AddOptions{ID: c.id, SourceDir: skillSource(t, c.id, nil), Group: c.group}); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := mgr.List("")
	if len(all) != 3 || all[0].ID != "alpha" || all[1].ID != "mid" || all[2].ID != "zeta" {
		t.Fatalf("all = %+v", all)
	}
	grpA, _ := mgr.List("a")
	if len(grpA) != 2 || grpA[0].ID != "mid" || grpA[1].ID != "zeta" {
		t.Fatalf("group a = %+v", grpA)
	}
}

func TestManagerShowDetectsTamperedArchive(t *testing.T) {
	mgr, store := newTestManager(t)
	if _, err := mgr.Add(AddOptions{ID: "alpha", SourceDir: skillSource(t, "alpha", nil)}); err != nil {
		t.Fatal(err)
	}
	key, _ := store.DeriveKeyFromPassword("pw")
	// 密文被同步端替换成「合法加密但内容越权」的归档：Show 必须拒绝。
	evil := buildTar(t,
		rawEntry{hdr: tar.Header{Typeflag: tar.TypeReg, Name: "SKILL.md", Mode: 0o644}, body: "---\nname: alpha\n---\n"},
		rawEntry{hdr: tarSymlinkHeader("link", "/etc/passwd")})
	if err := store.SaveSkillWithKey("default", "alpha", evil, key); err != nil {
		t.Fatal(err)
	}
	_, err := mgr.Show("alpha")
	if err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("Show error = %v, want unsupported type rejection", err)
	}
}

func TestManagerGroupCRUDAndPolicyInheritance(t *testing.T) {
	mgr, _ := newTestManager(t)
	yes, no := true, false

	if _, err := mgr.AddGroup(GroupOptions{
		Name:           "base",
		Description:    "global base",
		DefaultInstall: &yes,
		InstallScope:   storage.SkillScopeGlobal,
		Targets:        []string{"agents"},
	}); err != nil {
		t.Fatalf("AddGroup base: %v", err)
	}
	if _, err := mgr.AddGroup(GroupOptions{Name: "base", Description: "dup"}); err == nil {
		t.Fatal("duplicate group add must fail")
	}
	if _, err := mgr.AddGroup(GroupOptions{Name: "proj", Description: "zilta", DefaultInstall: &yes, InstallScope: storage.SkillScopeProject, InstallPath: "~/zilta"}); err != nil {
		t.Fatalf("AddGroup proj: %v", err)
	}

	if _, err := mgr.Add(AddOptions{ID: "s-base", SourceDir: skillSource(t, "s-base", nil), Group: "base"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Add(AddOptions{ID: "s-proj", SourceDir: skillSource(t, "s-proj", nil), Group: "proj"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Add(AddOptions{ID: "s-manual", SourceDir: skillSource(t, "s-manual", nil), Group: "base", DefaultInstall: &no}); err != nil {
		t.Fatal(err)
	}

	// 非空组不可删
	if err := mgr.DeleteGroup("base"); err == nil {
		t.Fatal("deleting non-empty group must fail")
	}
	if err := mgr.DeleteGroup("default"); err == nil {
		t.Fatal("deleting default group must fail")
	}

	// ListWithPolicy 过滤
	defaults, _, err := mgr.ListWithPolicy(ListFilter{DefaultOnly: true})
	if err != nil || len(defaults) != 2 {
		t.Fatalf("defaults = %+v err=%v", defaults, err)
	}
	projOnly, _, err := mgr.ListWithPolicy(ListFilter{Scope: storage.SkillScopeProject})
	if err != nil || len(projOnly) != 1 || projOnly[0].Entry.ID != "s-proj" || projOnly[0].Policy.InstallPath != "~/zilta" {
		t.Fatalf("projOnly = %+v err=%v", projOnly, err)
	}

	// EditGroup 修改组策略立即反映在继承条目上
	if _, err := mgr.EditGroup(GroupEditOptions{Name: "proj", InstallPathSet: true, InstallPath: "~/code/zilta"}); err != nil {
		t.Fatalf("EditGroup: %v", err)
	}
	_, pol, err := mgr.GetWithPolicy("s-proj")
	if err != nil || pol.InstallPath != "~/code/zilta" {
		t.Fatalf("inherited updated path = %+v err=%v", pol, err)
	}

	// 清空组后可删
	if _, err := mgr.AddGroup(GroupOptions{Name: "empty", Description: "tmp"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.DeleteGroup("empty"); err != nil {
		t.Fatalf("DeleteGroup empty: %v", err)
	}
}

func TestManagerEditMetadataAndMoveGroup(t *testing.T) {
	mgr, store := newTestManager(t)
	if _, err := mgr.Add(AddOptions{ID: "alpha", SourceDir: skillSource(t, "alpha", map[string]string{"extra.txt": "keep-me"}), Group: "g1", Description: "old"}); err != nil {
		t.Fatal(err)
	}
	yes := true
	newGrp := "g2"
	newDesc := "new desc"
	edited, err := mgr.Edit(EditOptions{
		ID:                "alpha",
		Group:             &newGrp,
		Description:       &newDesc,
		DefaultInstallSet: true,
		DefaultInstall:    &yes,
		InstallScopeSet:   true,
		InstallScope:      storage.SkillScopeProject,
		InstallPathSet:    true,
		InstallPath:       "~/code",
		TargetsSet:        true,
		Targets:           []string{"agents"},
	})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if edited.Group != "g2" || edited.Description != "new desc" || edited.InstallScope != storage.SkillScopeProject || edited.InstallPath != "~/code" {
		t.Fatalf("edited = %+v", edited)
	}
	// 密文迁移且归档内容完整保留
	if _, err := os.Stat(filepath.Join(store.GetDataPath(), "skills", "g1", "alpha.enc")); !os.IsNotExist(err) {
		t.Fatalf("old ciphertext must be deleted, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(store.GetDataPath(), "skills", "g2", "alpha.enc")); err != nil {
		t.Fatalf("new ciphertext missing: %v", err)
	}
	detail, err := mgr.Show("alpha")
	if err != nil || !detail.Policy.DefaultInstall || detail.Policy.InstallPath != "~/code" {
		t.Fatalf("detail after edit = %+v err=%v", detail, err)
	}
	foundExtra := false
	for _, f := range detail.Entries {
		if f.Path == "extra.txt" {
			foundExtra = true
		}
	}
	if !foundExtra {
		t.Fatal("Edit must not lose archive contents")
	}

	// 清除回继承
	cleared, err := mgr.Edit(EditOptions{
		ID:                "alpha",
		DefaultInstallSet: true,
		DefaultInstall:    nil,
		InstallScopeSet:   true,
		InstallScope:      "",
		InstallPathSet:    true,
		InstallPath:       "",
		TargetsSet:        true,
		Targets:           nil,
	})
	if err != nil {
		t.Fatalf("Edit clear: %v", err)
	}
	if cleared.DefaultInstall != nil || cleared.InstallScope != "" || cleared.InstallPath != "" || len(cleared.Targets) != 0 {
		t.Fatalf("cleared = %+v", cleared)
	}
}

func TestManagerVersioningLifecycle(t *testing.T) {
	mgr, store := newTestManager(t)

	// 1. 新入库无显式版本 -> 自动分配 v1
	e1, err := mgr.Add(AddOptions{ID: "alpha", SourceDir: skillSource(t, "alpha", nil), Note: "initial"})
	if err != nil {
		t.Fatalf("add v1: %v", err)
	}
	if e1.CurrentVersion != "v1" || len(e1.Versions) != 1 || e1.Versions[0].Note != "initial" {
		t.Fatalf("e1 = %+v", e1)
	}

	// 2. 显式新版本无需 --force 即可发布并激活
	e2, err := mgr.Add(AddOptions{
		ID:        "alpha",
		SourceDir: skillSource(t, "alpha", map[string]string{"extra.txt": "v2"}),
		Version:   "1.1.0",
		Note:      "add extra",
	})
	if err != nil {
		t.Fatalf("add 1.1.0: %v", err)
	}
	if e2.CurrentVersion != "1.1.0" || len(e2.Versions) != 2 {
		t.Fatalf("e2 = %+v", e2)
	}

	// 同版本无 --force 报错
	if _, err := mgr.Add(AddOptions{ID: "alpha", SourceDir: skillSource(t, "alpha", nil), Version: "1.1.0"}); err == nil {
		t.Fatal("duplicate explicit version without --force must fail")
	}

	// 3. --force 同内容无 --note 不递增版本号；新内容自动递增 v2
	sameForce, err := mgr.Add(AddOptions{
		ID:        "alpha",
		SourceDir: skillSource(t, "alpha", map[string]string{"extra.txt": "v2"}),
		Force:     true,
	})
	if err != nil || sameForce.CurrentVersion != "1.1.0" || len(sameForce.Versions) != 2 {
		t.Fatalf("same content force = %+v, err=%v", sameForce, err)
	}
	autoInc, err := mgr.Add(AddOptions{
		ID:        "alpha",
		SourceDir: skillSource(t, "alpha", map[string]string{"extra.txt": "v3"}),
		Force:     true,
	})
	if err != nil || autoInc.CurrentVersion != "v2" || len(autoInc.Versions) != 3 {
		t.Fatalf("auto increment = %+v, err=%v", autoInc, err)
	}

	// 4. ShowVersion / ArchiveVersion / ListVersions
	vers, cur, err := mgr.ListVersions("alpha")
	if err != nil || cur != "v2" || len(vers) != 3 {
		t.Fatalf("ListVersions = (%+v, %q, %v)", vers, cur, err)
	}
	detailV1, err := mgr.ShowVersion("alpha", "v1")
	if err != nil || detailV1.DisplayVersion != "v1" {
		t.Fatalf("ShowVersion(v1) = %+v, err=%v", detailV1, err)
	}
	_, archV1, resolvedV1, err := mgr.ArchiveVersion("alpha", "v1")
	if err != nil || resolvedV1 != "v1" || len(archV1) == 0 {
		t.Fatalf("ArchiveVersion(v1) = (%q, %v)", resolvedV1, err)
	}

	// 5. UseVersion 切换回 v1，并验证 RemoveVersion 保护当前活跃版
	used, err := mgr.UseVersion("alpha", "v1")
	if err != nil || used.CurrentVersion != "v1" {
		t.Fatalf("UseVersion(v1) = %+v, err=%v", used, err)
	}
	if _, err := mgr.RemoveVersion("alpha", "v1"); err == nil {
		t.Fatal("RemoveVersion on active version must fail")
	}
	if _, err := mgr.RemoveVersion("alpha", "1.1.0"); err != nil {
		t.Fatalf("RemoveVersion(1.1.0): %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.GetDataPath(), "skills", "default", "alpha@1.1.0.enc")); !os.IsNotExist(err) {
		t.Fatalf("removed version ciphertext still exists: %v", err)
	}

	// 6. Edit 改组迁移所有剩余版本，且新组可继续 UseVersion
	opsGroup := "ops"
	if _, err := mgr.Edit(EditOptions{ID: "alpha", Group: &opsGroup}); err != nil {
		t.Fatalf("Edit group: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.GetDataPath(), "skills", "default", "alpha@v2.enc")); !os.IsNotExist(err) {
		t.Fatalf("old group version ciphertext must be removed: %v", err)
	}
	if _, err := mgr.UseVersion("alpha", "v2"); err != nil {
		t.Fatalf("UseVersion(v2) after group move: %v", err)
	}

	// 7. Remove 清理全部版本密文
	if err := mgr.Remove("alpha"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	for _, f := range []string{"alpha.enc", "alpha@v1.enc", "alpha@v2.enc"} {
		if _, err := os.Stat(filepath.Join(store.GetDataPath(), "skills", "ops", f)); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after Remove: %v", f, err)
		}
	}
}

func TestManagerLegacyUnversionedUpgradePreservesSnapshot(t *testing.T) {
	mgr, store := newTestManager(t)
	key, err := store.DeriveKeyFromPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	oldArch, err := Pack(skillSource(t, "legacy", nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSkillWithKey("default", "legacy", oldArch, key); err != nil {
		t.Fatal(err)
	}
	idx := storage.NewSkillIndex()
	now := time.Now().Add(-time.Hour)
	idx.Skills["legacy"] = storage.SkillEntry{
		ID:            "legacy",
		Group:         "default",
		EncryptedFile: storage.SkillEncryptedFile("default", "legacy"),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.SaveSkillIndexWithKey(idx, key); err != nil {
		t.Fatal(err)
	}

	// 用新内容 + --version v2 升级，应自动把旧内容留存为 v0
	upgraded, err := mgr.Add(AddOptions{
		ID:        "legacy",
		SourceDir: skillSource(t, "legacy", map[string]string{"new.txt": "new"}),
		Version:   "v2",
	})
	if err != nil {
		t.Fatalf("upgrade legacy: %v", err)
	}
	if upgraded.CurrentVersion != "v2" || len(upgraded.Versions) != 2 || upgraded.Versions[0].Version != "v0" {
		t.Fatalf("upgraded = %+v", upgraded)
	}
	if _, err := mgr.UseVersion("legacy", "v0"); err != nil {
		t.Fatalf("rollback to preserved legacy v0: %v", err)
	}
}

func TestManagerFrontmatterVersion(t *testing.T) {
	mgr, _ := newTestManager(t)
	src := t.TempDir()
	writeTree(t, src, map[string]string{
		"SKILL.md": "---\nname: fm-skill\ndescription: x\nmetadata:\n  version: \"1.30\"\n---\nbody\n",
	})
	entry, err := mgr.Add(AddOptions{ID: "fm-skill", SourceDir: src})
	if err != nil {
		t.Fatalf("add with metadata.version: %v", err)
	}
	if entry.CurrentVersion != "1.30" {
		t.Fatalf("CurrentVersion = %q, want 1.30", entry.CurrentVersion)
	}
}

func TestManagerAddCompressibleOver1MiBAndShowApply(t *testing.T) {
	mgr, _ := newTestManager(t)
	src := t.TempDir()
	bigDoc := strings.Repeat("# Repeated Heading\nLine of skill documentation.\n", 30000)
	writeTree(t, src, map[string]string{
		"SKILL.md": "---\nname: big-skill\ndescription: compressible skill\n---\n\n# Big\n",
		"ref.md":   bigDoc,
	})
	entry, err := mgr.Add(AddOptions{ID: "big-skill", SourceDir: src})
	if err != nil {
		t.Fatalf("Add compressible >1MiB: %v", err)
	}
	if entry.CurrentVersion != "v1" {
		t.Fatalf("CurrentVersion = %q, want v1", entry.CurrentVersion)
	}

	detail, err := mgr.Show("big-skill")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if !detail.Compressed {
		t.Fatal("Show must report Compressed=true")
	}
	if detail.ArchiveSize >= detail.UncompressedSize || detail.UncompressedSize <= MaxArchiveBytes {
		t.Fatalf("unexpected sizes: compressed=%d uncompressed=%d", detail.ArchiveSize, detail.UncompressedSize)
	}

	_, archive, err := mgr.Archive("big-skill")
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	applier := NewApplier(t.TempDir())
	res, err := applier.Apply("big-skill", archive)
	if err != nil || res[0].Err != nil {
		t.Fatalf("Apply: %v %+v", err, res)
	}
	got, err := os.ReadFile(filepath.Join(res[0].Target, "ref.md"))
	if err != nil || string(got) != bigDoc {
		t.Fatalf("restored ref.md mismatch: len=%d err=%v", len(got), err)
	}
}
