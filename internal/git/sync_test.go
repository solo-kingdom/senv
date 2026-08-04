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

func TestPush_IncludesOutputOnFailure(t *testing.T) {
	_, machineA, machineB := setupRemotePair(t)

	if err := os.WriteFile(filepath.Join(machineB, "x.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, machineB, "add", ".")
	runGit(t, machineB, "commit", "-m", "x")
	runGit(t, machineB, "push")

	if err := os.WriteFile(filepath.Join(machineA, "y.txt"), []byte("y\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, machineA, "add", ".")
	runGit(t, machineA, "commit", "-m", "y")

	err := NewManager(machineA).Push()
	if err == nil {
		t.Fatal("expected push failure when remote ahead")
	}
	msg := err.Error()
	if !strings.Contains(msg, "push 失败") {
		t.Fatalf("expected push 失败 prefix, got: %v", err)
	}
	// Must surface git's rejection text, not only exit status.
	if strings.TrimSpace(msg) == "push 失败: exit status 1" {
		t.Fatalf("error should include git output, got: %q", msg)
	}
	if !strings.Contains(msg, "数据目录: "+machineA) {
		t.Fatalf("expected data dir in error, got: %q", msg)
	}
}
