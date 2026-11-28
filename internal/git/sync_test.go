package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireGit(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping git integration test in short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=senv-test",
		"GIT_AUTHOR_EMAIL=senv-test@example.com",
		"GIT_COMMITTER_NAME=senv-test",
		"GIT_COMMITTER_EMAIL=senv-test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// setupRemotePair creates a bare remote, a "machine A" clone with an initial
// commit, and a "machine B" clone tracking the same remote.
func setupRemotePair(t *testing.T) (remote, machineA, machineB string) {
	t.Helper()
	requireGit(t)

	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	machineA = filepath.Join(root, "a")
	machineB = filepath.Join(root, "b")

	if err := os.MkdirAll(remote, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, remote, "init", "--bare", "-b", "main")

	if err := os.MkdirAll(machineA, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, machineA, "init", "-b", "main")
	// Local identity so Manager.Commit works on CI (no global git user).
	runGit(t, machineA, "config", "user.name", "senv-test")
	runGit(t, machineA, "config", "user.email", "senv-test@example.com")
	runGit(t, machineA, "remote", "add", "origin", remote)
	if err := os.WriteFile(filepath.Join(machineA, "base.txt"), []byte("base\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, machineA, "add", ".")
	runGit(t, machineA, "commit", "-m", "initial")
	runGit(t, machineA, "push", "-u", "origin", "main")

	runGit(t, root, "clone", remote, machineB)
	runGit(t, machineB, "config", "user.name", "senv-test")
	runGit(t, machineB, "config", "user.email", "senv-test@example.com")
	return remote, machineA, machineB
}

func TestSync_AlreadyUpToDate(t *testing.T) {
	_, machineA, _ := setupRemotePair(t)
	mgr := NewManager(machineA)
	if err := mgr.Sync("unused"); err != nil {
		t.Fatalf("Sync on up-to-date repo: %v", err)
	}
}

func TestSync_RemoteAheadWithLocalChanges(t *testing.T) {
	_, machineA, machineB := setupRemotePair(t)

	// Machine B pushes a new commit first (simulates other machine).
	if err := os.WriteFile(filepath.Join(machineB, "from-b.txt"), []byte("b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, machineB, "add", ".")
	runGit(t, machineB, "commit", "-m", "from b")
	runGit(t, machineB, "push")

	// Machine A has local uncommitted work on a different file.
	if err := os.WriteFile(filepath.Join(machineA, "from-a.txt"), []byte("a\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(machineA)
	if err := mgr.Sync("from a"); err != nil {
		t.Fatalf("Sync should succeed with remote ahead + local changes: %v", err)
	}

	// Remote should contain both files.
	ls := exec.Command("git", "ls-tree", "-r", "--name-only", "HEAD")
	ls.Dir = machineA
	out, err := ls.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	names := string(out)
	if !strings.Contains(names, "from-a.txt") || !strings.Contains(names, "from-b.txt") {
		t.Fatalf("expected both files after sync, got:\n%s", names)
	}

	// Working tree clean and in sync with remote.
	has, err := mgr.HasChanges()
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("working tree should be clean after sync")
	}
	if err := mgr.Push(); !errors.Is(err, ErrNothingToPush) {
		t.Fatalf("expected ErrNothingToPush after sync, got %v", err)
	}
}

func TestSync_ConflictAborts(t *testing.T) {
	_, machineA, machineB := setupRemotePair(t)

	if err := os.WriteFile(filepath.Join(machineB, "conflict.txt"), []byte("b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, machineB, "add", ".")
	runGit(t, machineB, "commit", "-m", "b conflict")
	runGit(t, machineB, "push")

	if err := os.WriteFile(filepath.Join(machineA, "conflict.txt"), []byte("a\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(machineA)
	err := mgr.Sync("a conflict")
	if err == nil {
		t.Fatal("expected conflict error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "冲突") {
		t.Fatalf("expected conflict message, got: %v", err)
	}
	if !strings.Contains(msg, "数据目录: "+machineA) {
		t.Fatalf("expected data dir in error, got: %v", err)
	}
	if strings.Count(msg, "数据目录:") != 1 {
		t.Fatalf("data dir should appear once, got: %v", err)
	}

	// Rebase must not be left in progress.
	cmd := exec.Command("git", "rev-parse", "--git-path", "rebase-merge")
	cmd.Dir = machineA
	pathOut, _ := cmd.Output()
	rebasePath := filepath.Join(machineA, strings.TrimSpace(string(pathOut)))
	if _, statErr := os.Stat(rebasePath); statErr == nil {
		t.Fatal("rebase-merge should not exist after abort")
	}
}

func TestPush_RemoteAheadMergesAndPushes(t *testing.T) {
	_, machineA, machineB := setupRemotePair(t)

	if err := os.WriteFile(filepath.Join(machineB, "from-b.txt"), []byte("b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, machineB, "add", ".")
	runGit(t, machineB, "commit", "-m", "from b")
	runGit(t, machineB, "push")

	if err := os.WriteFile(filepath.Join(machineA, "from-a.txt"), []byte("a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, machineA, "add", ".")
	runGit(t, machineA, "commit", "-m", "from a")

	mgr := NewManager(machineA)
	if err := mgr.Push(); err != nil {
		t.Fatalf("Push should merge remote ahead then succeed: %v", err)
	}

	ls := exec.Command("git", "ls-tree", "-r", "--name-only", "HEAD")
	ls.Dir = machineA
	out, err := ls.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	names := string(out)
	if !strings.Contains(names, "from-a.txt") || !strings.Contains(names, "from-b.txt") {
		t.Fatalf("expected both files after merge push, got:\n%s", names)
	}
	assertNoMergeInProgress(t, machineA)
	if err := mgr.Push(); !errors.Is(err, ErrNothingToPush) {
		t.Fatalf("expected ErrNothingToPush after successful push, got %v", err)
	}
}

func TestPush_MergeConflictAborts(t *testing.T) {
	_, machineA, machineB := setupRemotePair(t)

	if err := os.WriteFile(filepath.Join(machineB, "conflict.txt"), []byte("b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, machineB, "add", ".")
	runGit(t, machineB, "commit", "-m", "b conflict")
	runGit(t, machineB, "push")

	if err := os.WriteFile(filepath.Join(machineA, "conflict.txt"), []byte("a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, machineA, "add", ".")
	runGit(t, machineA, "commit", "-m", "a conflict")

	err := NewManager(machineA).Push()
	if err == nil {
		t.Fatal("expected merge conflict error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "冲突") {
		t.Fatalf("expected conflict message, got: %v", err)
	}
	if !strings.Contains(msg, "数据目录: "+machineA) {
		t.Fatalf("expected data dir in error, got: %v", err)
	}
	if strings.Count(msg, "数据目录:") != 1 {
		t.Fatalf("data dir should appear once, got: %v", err)
	}
	assertNoMergeInProgress(t, machineA)

	// Local commit must still be present; remote must not have received the push.
	show := exec.Command("git", "log", "--oneline", "-1")
	show.Dir = machineA
	logOut, logErr := show.CombinedOutput()
	if logErr != nil {
		t.Fatal(logErr)
	}
	if !strings.Contains(string(logOut), "a conflict") {
		t.Fatalf("local commit should remain after abort, got: %s", logOut)
	}
}

func TestPush_DirtyWorktreeRefusesMerge(t *testing.T) {
	_, machineA, machineB := setupRemotePair(t)

	if err := os.WriteFile(filepath.Join(machineB, "from-b.txt"), []byte("b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, machineB, "add", ".")
	runGit(t, machineB, "commit", "-m", "from b")
	runGit(t, machineB, "push")

	if err := os.WriteFile(filepath.Join(machineA, "from-a.txt"), []byte("a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, machineA, "add", ".")
	runGit(t, machineA, "commit", "-m", "from a")
	if err := os.WriteFile(filepath.Join(machineA, "dirty.txt"), []byte("dirty\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := NewManager(machineA).Push()
	if err == nil {
		t.Fatal("expected dirty worktree to refuse merge")
	}
	msg := err.Error()
	if !strings.Contains(msg, "未提交") {
		t.Fatalf("expected dirty-worktree message, got: %v", err)
	}
	assertNoMergeInProgress(t, machineA)
}

func TestPush_IncludesOutputOnFailure(t *testing.T) {
	remote, machineA, _ := setupRemotePair(t)

	if err := os.WriteFile(filepath.Join(machineA, "y.txt"), []byte("y\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, machineA, "add", ".")
	runGit(t, machineA, "commit", "-m", "y")

	hook := filepath.Join(remote, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho 'hook rejected the push'\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}

	err := NewManager(machineA).Push()
	if err == nil {
		t.Fatal("expected push failure when remote hook rejects")
	}
	msg := err.Error()
	if !strings.Contains(msg, "push 失败") {
		t.Fatalf("expected push 失败 prefix, got: %v", err)
	}
	if strings.TrimSpace(msg) == "push 失败: exit status 1" {
		t.Fatalf("error should include git output, got: %q", msg)
	}
	if !strings.Contains(msg, "hook rejected the push") {
		t.Fatalf("error should include hook output, got: %q", msg)
	}
	if !strings.Contains(msg, "数据目录: "+machineA) {
		t.Fatalf("expected data dir in error, got: %q", msg)
	}
}

func assertNoMergeInProgress(t *testing.T, repo string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(repo, ".git", "MERGE_HEAD")); err == nil {
		t.Fatal("MERGE_HEAD should not exist")
	}
}

// writeMachineLocalArtifacts 模拟旧版本 vault：没有生成的 .gitignore，
// .senv-vault.lock 与 mcp-exports.json 以未跟踪文件长期存在。
func writeMachineLocalArtifacts(t *testing.T, repo string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, ".senv-vault.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "mcp-exports.json"), []byte(`{"cursor":{"github":"fp"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestSync_OnlyMachineLocalArtifactsIsNoop 复现 mini 事故：
// 只有机器本地工件的未跟踪状态时，Sync 曾先判定「有改动」、Add 后暂存区为空、
// 在 Commit 处以「没有需要提交的更改」中止，连远端更新都拉不下来。
func TestSync_OnlyMachineLocalArtifactsIsNoop(t *testing.T) {
	_, machineA, machineB := setupRemotePair(t)

	// 远端先行提交：修复前 sync 在 commit 处就失败，根本走不到 pull。
	if err := os.WriteFile(filepath.Join(machineB, "from-b.txt"), []byte("b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, machineB, "add", ".")
	runGit(t, machineB, "commit", "-m", "from b")
	runGit(t, machineB, "push")

	writeMachineLocalArtifacts(t, machineA)

	mgr := NewManager(machineA)
	if err := mgr.Sync("should be a no-op"); err != nil {
		t.Fatalf("Sync with only machine-local artifacts must succeed: %v", err)
	}

	// 远端更新已经拉下来（证明 pull 真正执行了）。
	if _, err := os.Stat(filepath.Join(machineA, "from-b.txt")); err != nil {
		t.Fatalf("expected remote update to be pulled: %v", err)
	}

	// 机器本地工件不得被提交。
	tracked, err := exec.Command("git", "-C", machineA, "ls-files").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".senv-vault.lock", "mcp-exports.json"} {
		if strings.Contains(string(tracked), name) {
			t.Fatalf("%s must never be committed, tracked files:\n%s", name, tracked)
		}
	}
}

// TestPull_IgnoresMachineLocalArtifacts 覆盖单独的 pull 路径：机器本地工件
// 不应触发「存在未提交的更改」。
func TestPull_IgnoresMachineLocalArtifacts(t *testing.T) {
	_, machineA, machineB := setupRemotePair(t)

	if err := os.WriteFile(filepath.Join(machineB, "from-b.txt"), []byte("b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, machineB, "add", ".")
	runGit(t, machineB, "commit", "-m", "from b")
	runGit(t, machineB, "push")

	writeMachineLocalArtifacts(t, machineA)

	if err := NewManager(machineA).Pull(); err != nil {
		t.Fatalf("Pull with only machine-local artifacts must succeed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(machineA, "from-b.txt")); err != nil {
		t.Fatalf("expected remote update to be pulled: %v", err)
	}
}

// TestAddCommitPush_OnlyMachineLocalArtifactsSkipsCommit 覆盖 push 语义：
// 没有可提交内容时跳过 commit 直接 push，而不是报「没有需要提交的更改」。
func TestAddCommitPush_OnlyMachineLocalArtifactsSkipsCommit(t *testing.T) {
	_, machineA, _ := setupRemotePair(t)

	before, err := exec.Command("git", "-C", machineA, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}

	writeMachineLocalArtifacts(t, machineA)

	err = NewManager(machineA).AddCommitPush("should not create a commit")
	if !errors.Is(err, ErrNothingToPush) {
		t.Fatalf("expected ErrNothingToPush (no commit, nothing to push), got %v", err)
	}

	after, err := exec.Command("git", "-C", machineA, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("HEAD moved: machine-local artifacts must not be committed (%s -> %s)", before, after)
	}
}

// TestHasSyncableChangesIgnoresMachineLocalArtifacts 锁定判定语义：
// HasChanges 看原始工作树，HasSyncableChanges 只看会被 Add 暂存的部分。
func TestHasSyncableChangesIgnoresMachineLocalArtifacts(t *testing.T) {
	m := initRealRepo(t)

	writeMachineLocalArtifacts(t, m.repoPath)

	hasChanges, err := m.HasChanges()
	if err != nil {
		t.Fatal(err)
	}
	if !hasChanges {
		t.Fatal("HasChanges should report untracked machine-local artifacts")
	}

	hasSyncable, err := m.HasSyncableChanges()
	if err != nil {
		t.Fatal(err)
	}
	if hasSyncable {
		t.Fatal("HasSyncableChanges must ignore machine-local artifacts")
	}

	// 真实改动仍然可见。
	if err := os.WriteFile(filepath.Join(m.repoPath, "normal.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hasSyncable, err = m.HasSyncableChanges()
	if err != nil {
		t.Fatal(err)
	}
	if !hasSyncable {
		t.Fatal("HasSyncableChanges must report regular changes")
	}
}
