package skill

import (
	"archive/tar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func demoArchive(t *testing.T, extra map[string]string) []byte {
	t.Helper()
	src := t.TempDir()
	files := map[string]string{"SKILL.md": goodSkillMD}
	for k, v := range extra {
		files[k] = v
	}
	writeTree(t, src, files)
	archive, err := Pack(src)
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

func newTestApplier(t *testing.T) *Applier {
	t.Helper()
	a := NewApplier(t.TempDir())
	a.Now = func() time.Time { return time.Date(2026, 10, 23, 1, 2, 3, 4, time.UTC) }
	return a
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestResolveRoots(t *testing.T) {
	roots, err := ResolveRoots(nil)
	if err != nil || len(roots) != 2 || roots[0] != ".agents/skills" {
		t.Fatalf("default: %v %v", roots, err)
	}
	roots, err = ResolveRoots([]string{"all"})
	if err != nil || len(roots) != 2 {
		t.Fatalf("all: %v %v", roots, err)
	}
	roots, err = ResolveRoots([]string{"claude", "agents", "claude"})
	if err != nil || len(roots) != 2 || roots[0] != ".claude/skills" || roots[1] != ".agents/skills" {
		t.Fatalf("order+dedupe: %v %v", roots, err)
	}
	roots, err = ResolveRoots([]string{"agents,claude"})
	if err != nil || len(roots) != 2 {
		t.Fatalf("comma: %v %v", roots, err)
	}
	if _, err := ResolveRoots([]string{"cursor"}); err == nil {
		t.Fatal("unknown target must fail")
	}
}

func TestApplyCreatesBothRootsWithOwnerMarker(t *testing.T) {
	a := newTestApplier(t)
	src := t.TempDir()
	writeTree(t, src, map[string]string{"SKILL.md": goodSkillMD, "scripts/run.sh": "#!/bin/sh\n"})
	if err := os.Chmod(filepath.Join(src, "scripts", "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive, err := Pack(src)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := a.Plan("demo", archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 || plan[0].Action != ActionCreate || plan[1].Action != ActionCreate {
		t.Fatalf("plan = %+v", plan)
	}
	// Plan 不写盘
	for _, root := range a.Roots {
		if _, err := os.Lstat(filepath.Join(a.rootDir(root))); !os.IsNotExist(err) {
			t.Fatalf("plan touched %s: %v", root, err)
		}
	}

	results, err := a.Apply("demo", archive)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Err != nil || r.Action != ActionCreate {
			t.Fatalf("result = %+v", r)
		}
		if got := readFile(t, filepath.Join(r.Target, "SKILL.md")); got != goodSkillMD {
			t.Fatalf("SKILL.md = %q", got)
		}
		if fi, err := os.Lstat(filepath.Join(r.Target, OwnedMarker)); err != nil || !fi.Mode().IsRegular() || fi.Size() != 0 {
			t.Fatalf("%s must be an empty regular file: %v", OwnedMarker, err)
		}
		fi, err := os.Stat(filepath.Join(r.Target, "scripts", "run.sh"))
		if err != nil || fi.Mode().Perm()&0o100 == 0 {
			t.Fatalf("exec bit lost: %v %v", fi, err)
		}
	}
	// 再来一次：内容等价 → skip（比较时排除 .senv-owned）
	plan, err = a.Plan("demo", archive)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range plan {
		if it.Action != ActionSkip {
			t.Fatalf("second plan = %+v", plan)
		}
	}
	// 临时目录不残留
	if entries, _ := os.ReadDir(filepath.Join(a.StateDir, "tmp")); len(entries) != 0 {
		t.Fatalf("staging leftovers: %v", entries)
	}
}

func TestApplySkipsSymlinkAndUnownedDirectories(t *testing.T) {
	a := newTestApplier(t)
	archive := demoArchive(t, nil)

	// .agents 根里是软链（指向用户自己的目录），.claude 根里是无标记目录
	userDir := t.TempDir()
	writeTree(t, userDir, map[string]string{"SKILL.md": "user's own"})
	agents := a.rootDir(".agents/skills")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(userDir, filepath.Join(agents, "demo")); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(a.rootDir(".claude/skills"), "demo")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTree(t, claude, map[string]string{"SKILL.md": "hand written"})

	results, err := a.Apply("demo", archive)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Action != ActionSkipConflict || r.Err != nil {
			t.Fatalf("result = %+v", r)
		}
	}
	if got := readFile(t, filepath.Join(userDir, "SKILL.md")); got != "user's own" {
		t.Fatalf("symlink target modified: %q", got)
	}
	if fi, err := os.Lstat(filepath.Join(agents, "demo")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink replaced: %v %v", fi, err)
	}
	if got := readFile(t, filepath.Join(claude, "SKILL.md")); got != "hand written" {
		t.Fatalf("unowned directory modified: %q", got)
	}
	if _, err := os.Stat(filepath.Join(a.StateDir, "backups")); !os.IsNotExist(err) {
		t.Fatalf("skip-conflict must not create backups: %v", err)
	}
}

func TestApplyOwnedMarkerMustBeRegularFile(t *testing.T) {
	a := newTestApplier(t)
	target := filepath.Join(a.rootDir(".agents/skills"), "demo")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "marker")
	if err := os.WriteFile(other, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(target, OwnedMarker)); err != nil {
		t.Fatal(err)
	}
	plan, err := a.Plan("demo", demoArchive(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if plan[0].Action != ActionSkipConflict {
		t.Fatalf("symlinked marker must not count as ownership: %+v", plan[0])
	}
}

func TestApplyBackupOverwriteKeepsBackupOutsideSkillRoots(t *testing.T) {
	a := newTestApplier(t)
	v1 := demoArchive(t, map[string]string{"notes.md": "v1"})
	if _, err := a.Apply("demo", v1); err != nil {
		t.Fatal(err)
	}
	// 用户在托管目录里加了文件，内容与归档不再一致
	target := filepath.Join(a.rootDir(".agents/skills"), "demo")
	writeTree(t, target, map[string]string{"local.txt": "local edit"})

	v2 := demoArchive(t, map[string]string{"notes.md": "v2"})
	results, err := a.Apply("demo", v2)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Err != nil || r.Action != ActionBackupOverwrite || r.Backup == "" {
			t.Fatalf("result = %+v", r)
		}
		for _, root := range a.Roots {
			if strings.HasPrefix(r.Backup, a.rootDir(root)+string(filepath.Separator)) {
				t.Fatalf("backup %s must be outside skills root %s", r.Backup, root)
			}
		}
		if !strings.HasPrefix(r.Backup, filepath.Join(a.StateDir, "backups")) || !strings.Contains(filepath.Base(r.Backup), "demo.senv-backup-") {
			t.Fatalf("unexpected backup path %s", r.Backup)
		}
		if got := readFile(t, filepath.Join(r.Target, "notes.md")); got != "v2" {
			t.Fatalf("new content not installed: %q", got)
		}
		if _, err := os.Stat(filepath.Join(r.Target, "local.txt")); !os.IsNotExist(err) {
			t.Fatalf("stale file survived overwrite: %v", err)
		}
		if _, err := os.Stat(filepath.Join(r.Target, OwnedMarker)); err != nil {
			t.Fatalf("marker missing after overwrite: %v", err)
		}
	}
	// .agents 根里的旧树（含用户加的文件）完整在备份里
	if got := readFile(t, filepath.Join(results[0].Backup, "local.txt")); got != "local edit" {
		t.Fatalf("backup content = %q", got)
	}
	if got := readFile(t, filepath.Join(results[0].Backup, "notes.md")); got != "v1" {
		t.Fatalf("backup notes = %q", got)
	}
	// skills 根下除了 demo 不能有别的东西（备份不能被 agent 当 skill 加载）
	for _, root := range a.Roots {
		entries, err := os.ReadDir(a.rootDir(root))
		if err != nil || len(entries) != 1 || entries[0].Name() != "demo" {
			t.Fatalf("root %s entries = %v err=%v", root, entries, err)
		}
	}
}

func TestApplyRevalidatesArchiveAndTouchesNothing(t *testing.T) {
	a := newTestApplier(t)
	bad := map[string][]byte{
		"symlink": buildTar(t, skillMDEntry(), rawEntry{hdr: tar.Header{Typeflag: tar.TypeSymlink, Name: "evil", Linkname: "/etc", Mode: 0o777}}),
		"dotdot":  buildTar(t, skillMDEntry(), rawEntry{hdr: tar.Header{Typeflag: tar.TypeReg, Name: "../escape", Mode: 0o644}, body: "x"}),
		"noname":  buildTar(t, rawEntry{hdr: tar.Header{Typeflag: tar.TypeReg, Name: "SKILL.md", Mode: 0o644}, body: "---\nname: other\n---\n"}),
		"reserved": buildTar(t, skillMDEntry(),
			rawEntry{hdr: tar.Header{Typeflag: tar.TypeReg, Name: OwnedMarker, Mode: 0o644}, body: ""}),
		"garbage": []byte("not a tar"),
	}
	for name, archive := range bad {
		if _, err := a.Plan("demo", archive); err == nil {
			t.Errorf("%s: Plan accepted a bad archive", name)
		}
		if _, err := a.Apply("demo", archive); err == nil {
			t.Errorf("%s: Apply accepted a bad archive", name)
		}
	}
	if _, err := os.Stat(a.StateDir); !os.IsNotExist(err) {
		t.Fatalf("rejected archives must not create state: %v", err)
	}
	for _, root := range a.Roots {
		if _, err := os.Lstat(a.rootDir(root)); !os.IsNotExist(err) {
			t.Fatalf("rejected archive touched %s: %v", root, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(a.Base), "escape")); !os.IsNotExist(err) {
		t.Fatalf("path traversal escaped: %v", err)
	}
}

func TestApplyOnlyKeepsPermBits(t *testing.T) {
	a := newTestApplier(t)
	archive := buildTar(t,
		skillMDEntry(),
		rawEntry{hdr: tar.Header{Typeflag: tar.TypeDir, Name: "sub/", Mode: 0o4777}},
		rawEntry{hdr: tar.Header{Typeflag: tar.TypeReg, Name: "sub/suid.sh", Mode: 0o6777}, body: "x"},
		rawEntry{hdr: tar.Header{Typeflag: tar.TypeReg, Name: "ro.txt", Mode: 0o000}, body: "y"},
	)
	results, err := a.Apply("demo", archive)
	if err != nil || results[0].Err != nil {
		t.Fatalf("apply: %v %+v", err, results)
	}
	target := results[0].Target
	fi, err := os.Stat(filepath.Join(target, "sub", "suid.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		t.Fatalf("special mode bits leaked: %v", fi.Mode())
	}
	if fi.Mode().Perm()&0o022 != 0 || fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("perm = %v, want exec kept and group/other write dropped", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Join(target, "sub"))
	if di.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || di.Mode().Perm()&0o700 != 0o700 {
		t.Fatalf("dir mode = %v", di.Mode())
	}
	// 权限为 0 的文件展开后属主仍可读写，下次 apply 的清理与比较不会卡住
	if _, err := os.ReadFile(filepath.Join(target, "ro.txt")); err != nil {
		t.Fatalf("owner must be able to read: %v", err)
	}
	if plan, _ := a.Plan("demo", archive); plan[0].Action != ActionSkip {
		t.Fatalf("re-plan = %+v", plan[0])
	}
}

func TestApplyDetectsSymlinkInsideManagedTree(t *testing.T) {
	a := newTestApplier(t)
	archive := demoArchive(t, nil)
	if _, err := a.Apply("demo", archive); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(a.rootDir(".agents/skills"), "demo")
	if err := os.Symlink("/nonexistent", filepath.Join(target, "link")); err != nil {
		t.Fatal(err)
	}
	plan, err := a.Plan("demo", archive)
	if err != nil {
		t.Fatal(err)
	}
	if plan[0].Action != ActionBackupOverwrite {
		t.Fatalf("a symlink inside the managed tree must count as drift: %+v", plan[0])
	}
}
