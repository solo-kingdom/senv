package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultTimeout is the default timeout for git operations
const DefaultTimeout = 60 * time.Second

// Manager handles git operations
type Manager struct {
	repoPath string
	timeout  time.Duration
}

// NewManager creates a new git manager
func NewManager(repoPath string) *Manager {
	return &Manager{
		repoPath: repoPath,
		timeout:  DefaultTimeout,
	}
}

// NewManagerWithTimeout creates a new git manager with custom timeout
func NewManagerWithTimeout(repoPath string, timeout time.Duration) *Manager {
	return &Manager{
		repoPath: repoPath,
		timeout:  timeout,
	}
}

// SetTimeout sets the timeout for git operations
func (m *Manager) SetTimeout(timeout time.Duration) {
	m.timeout = timeout
}

// runCommand executes a git command with context and timeout
func (m *Manager) runCommand(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = m.repoPath

	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("git 命令超时: %v", ctx.Err())
	}

	return output, err
}

// runCommandOutput executes a git command and returns only output (not stderr)
func (m *Manager) runCommandOutput(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = m.repoPath

	output, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("git 命令超时: %v", ctx.Err())
	}

	return output, err
}

// IsGitRepo checks if the path is a git repository
func (m *Manager) IsGitRepo() bool {
	gitDir := filepath.Join(m.repoPath, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// IsGitRoot checks if the path is the root of a git repository
func (m *Manager) IsGitRoot() bool {
	if !m.IsGitRepo() {
		return false
	}

	ctx := context.Background()
	output, err := m.runCommandOutput(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}

	gitRoot := strings.TrimSpace(string(output))
	absPath, err := filepath.Abs(m.repoPath)
	if err != nil {
		return false
	}

	return gitRoot == absPath
}

// Status returns the git status
func (m *Manager) Status() (string, error) {
	return m.StatusWithContext(context.Background())
}

// StatusWithContext returns the git status with context
func (m *Manager) StatusWithContext(ctx context.Context) (string, error) {
	output, err := m.runCommand(ctx, "status", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("failed to get git status: %w", err)
	}
	return string(output), nil
}

// HasChanges checks if there are uncommitted changes
func (m *Manager) HasChanges() (bool, error) {
	return m.HasChangesWithContext(context.Background())
}

// HasChangesWithContext checks if there are uncommitted changes with context
func (m *Manager) HasChangesWithContext(ctx context.Context) (bool, error) {
	status, err := m.StatusWithContext(ctx)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(status) != "", nil
}

// Pull performs a git pull operation
func (m *Manager) Pull() error {
	return m.PullWithContext(context.Background())
}

// PullWithContext performs a git pull operation with context
func (m *Manager) PullWithContext(ctx context.Context) error {
	hasChanges, err := m.HasChangesWithContext(ctx)
	if err != nil {
		return err
	}

	if hasChanges {
		return fmt.Errorf("无法 pull：存在未提交的更改。请先提交或暂存您的更改")
	}

	// Fetch first
	_, err = m.runCommand(ctx, "fetch")
	if err != nil {
		return fmt.Errorf("fetch 失败: %w", err)
	}

	// Check if we're behind the remote
	output, err := m.runCommand(ctx, "status", "-uno")
	if err != nil {
		return fmt.Errorf("检查状态失败: %w", err)
	}

	if strings.Contains(string(output), "behind") {
		output, err = m.runCommand(ctx, "pull", "--ff-only")
		if err != nil {
			if strings.Contains(string(output), "CONFLICT") || strings.Contains(string(output), "diverged") {
				return fmt.Errorf("pull 失败：本地和远程分支存在冲突。\n请手动解决冲突后重试。\n详细信息: %s", string(output))
			}
			return fmt.Errorf("pull 失败: %w\n%s", err, string(output))
		}
		return nil
	}

	return nil
}

// Add adds all changes to the staging area
func (m *Manager) Add() error {
	return m.AddWithContext(context.Background())
}

// AddWithContext adds all changes to the staging area with context
func (m *Manager) AddWithContext(ctx context.Context) error {
	_, err := m.runCommand(ctx, "add", ".")
	if err != nil {
		return fmt.Errorf("git add 失败: %w", err)
	}
	return nil
}

// Commit creates a commit with the given message
// Returns error if there are no staged changes
func (m *Manager) Commit(message string) error {
	return m.CommitWithContext(context.Background(), message)
}

// CommitWithContext creates a commit with the given message and context
func (m *Manager) CommitWithContext(ctx context.Context, message string) error {
	// Check if there are staged changes
	// git diff --cached --quiet returns exit code 0 if no staged changes, 1 if there are staged changes
	output, err := m.runCommand(ctx, "diff", "--cached", "--quiet")

	if err == nil {
		// Exit code 0 means no staged changes
		return fmt.Errorf("没有需要提交的更改。请先使用 'git add' 添加更改")
	}

	// There are staged changes, proceed with commit
	_, err = m.runCommand(ctx, "commit", "-m", message)
	if err != nil {
		return fmt.Errorf("commit 失败: %w", err)
	}

	_ = output // output is not used, we only care about the exit code
	return nil
}

// Push pushes commits to the remote repository
func (m *Manager) Push() error {
	return m.PushWithContext(context.Background())
}

// PushWithContext pushes commits to the remote repository with context
func (m *Manager) PushWithContext(ctx context.Context) error {
	// Check if there's a remote
	output, err := m.runCommandOutput(ctx, "remote")
	if err != nil {
		return fmt.Errorf("检查远程仓库失败: %w", err)
	}

	if strings.TrimSpace(string(output)) == "" {
		return fmt.Errorf("没有配置远程仓库。请先使用 'git remote add' 添加远程仓库")
	}

	// Get current branch
	branchOutput, err := m.runCommandOutput(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Errorf("获取当前分支失败: %w", err)
	}
	currentBranch := strings.TrimSpace(string(branchOutput))

	// Check if there are commits to push
	output, err = m.runCommandOutput(ctx, "log", "@{u}..HEAD", "--oneline")
	if err != nil {
		// Maybe no upstream set, try to push anyway
		_, err = m.runCommand(ctx, "push", "-u", "origin", currentBranch)
		if err != nil {
			return fmt.Errorf("push 失败: %w", err)
		}
		return nil
	}

	if strings.TrimSpace(string(output)) == "" {
		return fmt.Errorf("没有需要推送的提交")
	}

	// Push
	_, err = m.runCommand(ctx, "push")
	if err != nil {
		return fmt.Errorf("push 失败: %w", err)
	}

	return nil
}

// AddCommitPush performs add, commit, and push in one operation
func (m *Manager) AddCommitPush(message string) error {
	return m.AddCommitPushWithContext(context.Background(), message)
}

// AddCommitPushWithContext performs add, commit, and push in one operation with context
func (m *Manager) AddCommitPushWithContext(ctx context.Context, message string) error {
	if err := m.AddWithContext(ctx); err != nil {
		return err
	}

	if err := m.CommitWithContext(ctx, message); err != nil {
		return err
	}

	if err := m.PushWithContext(ctx); err != nil {
		return err
	}

	return nil
}

// GetCurrentBranch returns the current branch name
func (m *Manager) GetCurrentBranch() (string, error) {
	return m.GetCurrentBranchWithContext(context.Background())
}

// GetCurrentBranchWithContext returns the current branch name with context
func (m *Manager) GetCurrentBranchWithContext(ctx context.Context) (string, error) {
	output, err := m.runCommandOutput(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("获取当前分支失败: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// GetRemoteURL returns the remote URL
func (m *Manager) GetRemoteURL() (string, error) {
	return m.GetRemoteURLWithContext(context.Background())
}

// GetRemoteURLWithContext returns the remote URL with context
func (m *Manager) GetRemoteURLWithContext(ctx context.Context) (string, error) {
	output, err := m.runCommandOutput(ctx, "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("获取远程仓库 URL 失败: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// GetStatusInfo returns detailed status information
func (m *Manager) GetStatusInfo() (*StatusInfo, error) {
	return m.GetStatusInfoWithContext(context.Background())
}

// GetStatusInfoWithContext returns detailed status information with context
func (m *Manager) GetStatusInfoWithContext(ctx context.Context) (*StatusInfo, error) {
	info := &StatusInfo{
		Path: m.repoPath,
	}

	if !m.IsGitRepo() {
		info.IsGitRepo = false
		return info, nil
	}

	info.IsGitRepo = true
	info.IsGitRoot = m.IsGitRoot()

	if branch, err := m.GetCurrentBranchWithContext(ctx); err == nil {
		info.CurrentBranch = branch
	}

	if url, err := m.GetRemoteURLWithContext(ctx); err == nil {
		info.RemoteURL = url
	}

	hasChanges, err := m.HasChangesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	info.HasChanges = hasChanges

	return info, nil
}

// StatusInfo contains git status information
type StatusInfo struct {
	Path          string
	IsGitRepo     bool
	IsGitRoot     bool
	CurrentBranch string
	RemoteURL     string
	HasChanges    bool
}
