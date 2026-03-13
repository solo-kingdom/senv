package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/git"
)

var gitCmd = &cobra.Command{
	Use:   "git",
	Short: "Git repository operations",
	Long:  `Manage git operations for the encrypted configuration data.`,
}

var (
	gitCommitMessage string
)

func init() {
	rootCmd.AddCommand(gitCmd)
}

// getGitManager creates a git manager for the data path
func getGitManager() (*git.Manager, error) {
	dataPath := getDataPath()
	manager := git.NewManager(dataPath)

	// Check if it's a git repository
	if !manager.IsGitRepo() {
		return nil, fmt.Errorf("数据路径 '%s' 不是 git 仓库。\n请先初始化 git 仓库:\n  cd %s\n  git init", dataPath, dataPath)
	}

	// Check if it's the git root
	if !manager.IsGitRoot() {
		return nil, fmt.Errorf("数据路径 '%s' 不是 git 仓库的根目录。\n请使用 git 仓库的根目录作为数据路径", dataPath)
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
		return nil
	},
}

// gitPushCmd represents the git push command
var gitPushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push changes to remote repository",
	Long:  `Push committed changes to the remote repository.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := getGitManager()
		if err != nil {
			return err
		}

		fmt.Printf("正在推送到远程仓库...\n")
		if err := manager.Push(); err != nil {
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
		dataPath := getDataPath()
		manager := git.NewManager(dataPath)

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

func init() {
	gitCmd.AddCommand(gitPullCmd)
	gitCmd.AddCommand(gitPushCmd)
	gitCmd.AddCommand(gitCommitCmd)
	gitCmd.AddCommand(gitSyncCmd)
	gitCmd.AddCommand(gitStatusCmd)

	gitCommitCmd.Flags().StringVarP(&gitCommitMessage, "message", "m", "", "commit message")
	gitSyncCmd.Flags().StringVarP(&gitCommitMessage, "message", "m", "", "commit message (default: auto-generated)")
}
