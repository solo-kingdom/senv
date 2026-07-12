package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/git"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

var gitCmd = &cobra.Command{
	Use:   "git",
	Short: "Git repository operations",
	Long:  `Manage git operations for the encrypted configuration data.`,
}

var (
	gitCommitMessage string
	gitPushOnly      bool
)

func init() {
	rootCmd.AddCommand(gitCmd)
}

// getGitManager creates a git manager for the git path (common parent of config and data)
func getGitManager() (*git.Manager, error) {
	configPath := getConfigPath()
	dataPath := getDataPath()
	storageManager := storage.NewManager(configPath, dataPath)
	gitPath := storageManager.GetGitPath()

	manager := git.NewManager(gitPath)

	// Check if it's a git repository
	if !manager.IsGitRepo() {
		return nil, fmt.Errorf("数据路径 '%s' 不是 git 仓库。\n请先初始化 git 仓库:\n  cd %s\n  git init", gitPath, gitPath)
	}

	// Check if it's the git root
	if !manager.IsGitRoot() {
		return nil, fmt.Errorf("数据路径 '%s' 不是 git 仓库的根目录。\n请使用 git 仓库的根目录作为数据路径", gitPath)
	}

	return manager, nil
}

// gitPullCmd represents the git pull command
var gitPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull changes from remote repository",
	Long: `Pull changes from the remote repository.
This command will fail if there are uncommitted changes or merge conflicts.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := getGitManager()
		if err != nil {
			return err
		}

		fmt.Printf("正在从远程仓库拉取更新...\n")
		if err := manager.Pull(); err != nil {
			return err
		}

		fmt.Printf("✓ 成功拉取更新\n")
		// Best-effort consistency self-check: only when a session key is already
		// available, so we never force a password prompt after a pull.
		postPullSelfCheck(getConfigPath(), getDataPath(), os.Stdout)
		return nil
	},
}

// gitPushCmd represents the git push command
// Supports three modes:
//   - senv git push          → auto-generate commit message, confirm, then add+commit+push
//   - senv git push -m "msg" → use specified message to add+commit+push
//   - senv git push --only   → pure push (only push existing commits)
var gitPushCmd = &cobra.Command{
	Use:   "push [-m <message> | --only]",
	Short: "Push changes to remote repository",
	Long: `Push changes to the remote repository.

Without flags: auto-generate a commit message from changed files and ask for
confirmation before add + commit + push.

  -m, --message   Provide a commit message to skip confirmation (add+commit+push)
      --only      Only push existing commits, do not add or commit`,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := getGitManager()
		if err != nil {
			return err
		}

		// --only: pure push (original behavior)
		if gitPushOnly {
			fmt.Printf("正在推送到远程仓库...\n")
			if err := manager.Push(); err != nil {
				return err
			}
			fmt.Printf("✓ 成功推送更改\n")
			return nil
		}

		// -m provided: use specified message, add+commit+push directly
		if gitCommitMessage != "" {
			fmt.Printf("正在同步更改到远程仓库...\n")
			if err := manager.AddCommitPush(gitCommitMessage); err != nil {
				return err
			}
			fmt.Printf("✓ 成功推送更改 (commit: %s)\n", gitCommitMessage)
			return nil
		}

		// No flags: auto-generate message and ask for confirmation
		status, err := manager.Status()
		if err != nil {
			return err
		}
		if strings.TrimSpace(status) == "" {
			// No changes, just push
			fmt.Printf("没有待提交的更改，直接推送...\n")
			if err := manager.Push(); err != nil {
				return err
			}
			fmt.Printf("✓ 成功推送更改\n")
			return nil
		}

		// Build auto-generated commit message
		autoMessage := fmt.Sprintf("Update configurations - %s", time.Now().Format("2006-01-02 15:04:05"))

		// Show changes and ask for confirmation
		fmt.Printf("\n将提交以下更改:\n")
		fmt.Print(status)
		fmt.Printf("\nCommit message:\n  %s\n", autoMessage)
		fmt.Printf("\n确认推送？(y/N): ")

		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))

		if input != "y" && input != "yes" {
			fmt.Printf("已取消推送\n")
			return nil
		}

		fmt.Printf("\n正在同步更改到远程仓库...\n")
		if err := manager.AddCommitPush(autoMessage); err != nil {
			return err
		}
		fmt.Printf("✓ 成功推送更改\n")
		return nil
	},
}

// gitCommitCmd represents the git commit command
var gitCommitCmd = &cobra.Command{
	Use:   "commit -m <message>",
	Short: "Add and commit all changes",
	Long: `Add all changes to staging area and create a commit.
You must provide a commit message with -m flag.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := getGitManager()
		if err != nil {
			return err
		}

		if gitCommitMessage == "" {
			return fmt.Errorf("请提供提交信息，使用 -m 标志")
		}

		// Add changes
		fmt.Printf("正在添加更改...\n")
		if err := manager.Add(); err != nil {
			return err
		}

		// Commit
		fmt.Printf("正在提交更改...\n")
		if err := manager.Commit(gitCommitMessage); err != nil {
			return err
		}

		fmt.Printf("✓ 成功提交更改\n")
		return nil
	},
}

// gitSyncCmd represents the git sync command (add + commit + push)
var gitSyncCmd = &cobra.Command{
	Use:   "sync -m <message>",
	Short: "Add, commit, and push changes",
	Long: `Add all changes, create a commit, and push to remote repository.
This is a convenience command that combines add, commit, and push.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := getGitManager()
		if err != nil {
			return err
		}

		if gitCommitMessage == "" {
			// Generate default message with timestamp
			gitCommitMessage = fmt.Sprintf("Update configurations - %s", time.Now().Format("2006-01-02 15:04:05"))
		}

		fmt.Printf("正在同步更改到远程仓库...\n")
		if err := manager.AddCommitPush(gitCommitMessage); err != nil {
			return err
		}

		fmt.Printf("✓ 成功同步更改\n")
		return nil
	},
}

// gitStatusCmd represents the git status command
var gitStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show git status",
	Long:  `Show the current git status of the data repository.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := getConfigPath()
		dataPath := getDataPath()
		storageManager := storage.NewManager(configPath, dataPath)
		gitPath := storageManager.GetGitPath()
		manager := git.NewManager(gitPath)

		// Get status info
		info, err := manager.GetStatusInfo()
		if err != nil {
			return err
		}

		fmt.Printf("数据路径: %s\n", info.Path)

		if !info.IsGitRepo {
			fmt.Printf("状态: 不是 git 仓库\n")
			return nil
		}

		fmt.Printf("状态: git 仓库\n")
		fmt.Printf("是否为根目录: %v\n", info.IsGitRoot)

		if info.CurrentBranch != "" {
			fmt.Printf("当前分支: %s\n", info.CurrentBranch)
		}

		if info.RemoteURL != "" {
			fmt.Printf("远程仓库: %s\n", info.RemoteURL)
		}

		if info.HasChanges {
			fmt.Printf("待提交更改: 是\n")

			// Show detailed status
			status, err := manager.Status()
			if err != nil {
				return err
			}
			if status != "" {
				fmt.Printf("\n更改详情:\n")
				fmt.Print(status)
			}
		} else {
			fmt.Printf("待提交更改: 否\n")
		}

		return nil
	},
}

// pushShortcutCmd is a top-level shortcut: senv push == senv git push
var pushShortcutCmd = &cobra.Command{
	Use:   "push [-m <message> | --only]",
	Short: "Shortcut for 'senv git push'",
	Long:  `Equivalent to 'senv git push'. See 'senv git push --help' for details.`,
	RunE:  gitPushCmd.RunE,
}

func init() {
	gitCmd.AddCommand(gitPullCmd)
	gitCmd.AddCommand(gitPushCmd)
	gitCmd.AddCommand(gitCommitCmd)
	gitCmd.AddCommand(gitSyncCmd)
	gitCmd.AddCommand(gitStatusCmd)

	// Top-level shortcut
	rootCmd.AddCommand(pushShortcutCmd)

	gitCommitCmd.Flags().StringVarP(&gitCommitMessage, "message", "m", "", "commit message")
	gitSyncCmd.Flags().StringVarP(&gitCommitMessage, "message", "m", "", "commit message (default: auto-generated)")
	gitPushCmd.Flags().StringVarP(&gitCommitMessage, "message", "m", "", "commit message (add+commit+push)")
	gitPushCmd.Flags().BoolVar(&gitPushOnly, "only", false, "only push existing commits without add/commit")
	pushShortcutCmd.Flags().StringVarP(&gitCommitMessage, "message", "m", "", "commit message (add+commit+push)")
	pushShortcutCmd.Flags().BoolVar(&gitPushOnly, "only", false, "only push existing commits without add/commit")
}

// postPullSelfCheck runs a best-effort consistency check after `git pull`.
//
// It probes with the cached session key (via PeekCachedKey, which does not
// validate or clear) so it can detect a desync even though GetCachedKey would
// refuse the now-stale key. It NEVER prompts for a password: when no cache
// exists it stays silent. It only prints a warning pointing at `senv doctor`;
// it never modifies or deletes files and never returns an error (the pull
// already succeeded).
func postPullSelfCheck(configPath, dataPath string, out io.Writer) {
	sm := session.NewManager(configPath, dataPath)
	key, _, err := sm.PeekCachedKey()
	if err != nil {
		// No session cache: stay silent rather than prompting.
		return
	}
	store := storage.NewManager(configPath, dataPath)
	report, err := store.CheckConsistency(key)
	if err != nil || report.AllOK() {
		return
	}
	fmt.Fprintln(out, "⚠ 拉取后检测到 metadata 与部分数据文件不同步（可能无法解密）。")
	fmt.Fprintln(out, "  运行 `senv doctor` 查看详情。本命令未修改或删除任何文件。")
}
