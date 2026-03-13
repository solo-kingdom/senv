package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewManager(t *testing.T) {
	path := "/tmp/test"
	manager := NewManager(path)

	if manager.repoPath != path {
		t.Errorf("Expected repoPath %s, got %s", path, manager.repoPath)
	}
}

func TestIsGitRepo(t *testing.T) {
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)

	// Not a git repo yet
	if manager.IsGitRepo() {
		t.Error("Should not be a git repo initially")
	}

	// Create .git directory
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatalf("Failed to create .git dir: %v", err)
	}

	// Now it should be a git repo
	if !manager.IsGitRepo() {
		t.Error("Should be a git repo after creating .git directory")
	}
}

func TestIsGitRepoWithFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .git as a file (not a directory)
	gitFile := filepath.Join(tmpDir, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: /some/path"), 0644); err != nil {
		t.Fatalf("Failed to create .git file: %v", err)
	}

	manager := NewManager(tmpDir)

	// Should not be considered a git repo (it's a file, not a directory)
	if manager.IsGitRepo() {
		t.Error(".git file should not be considered a git repo")
	}
}

func TestStatusInfo(t *testing.T) {
	info := &StatusInfo{
		Path:          "/tmp/test",
		IsGitRepo:     true,
		IsGitRoot:     true,
		CurrentBranch: "main",
		RemoteURL:     "https://github.com/user/repo.git",
		HasChanges:    false,
	}

	if info.Path != "/tmp/test" {
		t.Errorf("Expected Path '/tmp/test', got %s", info.Path)
	}

	if !info.IsGitRepo {
		t.Error("IsGitRepo should be true")
	}

	if info.CurrentBranch != "main" {
		t.Errorf("Expected branch 'main', got %s", info.CurrentBranch)
	}
}

func TestGetStatusInfoNonGitRepo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)
	info, err := manager.GetStatusInfo()

	if err != nil {
		t.Fatalf("GetStatusInfo should not fail for non-git dir: %v", err)
	}

	if info.IsGitRepo {
		t.Error("IsGitRepo should be false for non-git directory")
	}

	if info.Path != tmpDir {
		t.Errorf("Expected Path %s, got %s", tmpDir, info.Path)
	}
}

// Note: The following tests require git to be installed and will modify the filesystem
// They are integration tests rather than pure unit tests

func TestGitCommandsInRealRepo(t *testing.T) {
	// Skip if running in CI or if git is not available
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if git is available
	if _, err := os.Stat("/usr/bin/git"); os.IsNotExist(err) {
		t.Skip("git not available")
	}

	tmpDir, err := os.MkdirTemp("", "git-integration-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager := NewManager(tmpDir)

	// Initially not a git repo
	if manager.IsGitRepo() {
		t.Error("Should not be a git repo initially")
	}

	// These operations should fail on non-git repo
	_, err = manager.Status()
	if err == nil {
		t.Error("Status should fail on non-git repo")
	}

	err = manager.Add()
	if err == nil {
		t.Error("Add should fail on non-git repo")
	}
}
