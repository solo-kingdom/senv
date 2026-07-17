package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/git"
	"github.com/wii/senv/internal/session"
)

var interactiveCmd = &cobra.Command{
	Use:   "interactive",
	Short: "Start interactive mode",
	Long: `Start an interactive shell for managing senv.
This provides a user-friendly menu-driven interface for all operations.`,
	RunE: runInteractive,
}

func init() {
	rootCmd.AddCommand(interactiveCmd)
}

func runInteractive(cmd *cobra.Command, args []string) error {
	configPath := getConfigPath()
	dataPath := getDataPath()

	auth, err := resolveAuth(configPath, dataPath, authPrompt)
	if err != nil {
		if errors.Is(err, errNotInitialized) {
			// Preserve the friendly interactive UX for the not-initialized case.
			fmt.Println("❌ 项目未初始化")
			fmt.Println("请先运行: senv init")
			return nil
		}
		return err
	}

	var envManager *env.Manager
	var configManager *config.Manager
	if auth.hasKey() {
		envManager = env.NewManagerWithKey(auth.storage, auth.key)
		configManager = config.NewManagerWithKey(auth.storage, auth.key)
	} else {
		envManager = env.NewManager(auth.storage, auth.password)
		configManager = config.NewManager(auth.storage, auth.password)
	}

	gitManager := git.NewManager(auth.storage.GetGitPath())

	is := &interactiveSession{
		reader:         bufio.NewReader(os.Stdin),
		storageManager: auth.storage,
		envManager:     envManager,
		configManager:  configManager,
		gitManager:     gitManager,
		sessionManager: session.NewManager(configPath, dataPath),
		password:       auth.password,
	}

	fmt.Println("\n✓ 登录成功")
	fmt.Println("欢迎使用 senv 交互模式")
	fmt.Println("输入 'q' 或 'quit' 退出，输入 'h' 或 'help' 查看帮助")

	return is.mainMenu()
}

func (is *interactiveSession) mainMenu() error {
	for {
		fmt.Println("\n╔════════════════════════════════════╗")
		fmt.Println("║        senv 主菜单                  ║")
		fmt.Println("╚════════════════════════════════════╝")
		fmt.Println("1. 环境变量管理")
		fmt.Println("2. 配置文件管理")
		fmt.Println("3. Git 操作")
		fmt.Println("4. 会话管理")
		fmt.Println("5. 查看状态")
		fmt.Println("q. 退出")

		choice := is.prompt("请选择 [1-5/q]: ")
		if choice == "" {
			continue
		}

		switch strings.ToLower(choice) {
		case "1":
			is.envMenu()
		case "2":
			is.configMenu()
		case "3":
			is.gitMenu()
		case "4":
			is.sessionMenu()
		case "5":
			is.showStatus()
		case "q", "quit", "exit":
			fmt.Println("\n再见！")
			return nil
		case "h", "help":
			is.showHelp()
		default:
			fmt.Println("❌ 无效选择，请重试")
		}
	}
}

func (is *interactiveSession) showStatus() {
	fmt.Println("\n╔════════════════════════════════════╗")
	fmt.Println("║        系统状态                     ║")
	fmt.Println("╚════════════════════════════════════╝")

	fmt.Printf("\n配置路径: %s\n", is.storageManager.GetConfigPath())
	fmt.Printf("数据路径: %s\n", is.storageManager.GetDataPath())

	gitInfo, _ := is.gitManager.GetStatusInfo()
	if gitInfo != nil && gitInfo.IsGitRepo {
		fmt.Printf("\nGit 状态:\n")
		fmt.Printf("  当前分支: %s\n", gitInfo.CurrentBranch)
		if gitInfo.RemoteURL != "" {
			fmt.Printf("  远程仓库: %s\n", gitInfo.RemoteURL)
		}
	}

	cache, _ := is.sessionManager.LoadCache()
	if cache != nil {
		valid, _ := is.sessionManager.IsCacheValid(cache)
		if valid {
			fmt.Printf("\n会话状态: 已登录\n")
		} else {
			fmt.Printf("\n会话状态: 已过期\n")
		}
	} else {
		fmt.Printf("\n会话状态: 未登录\n")
	}

	is.prompt("\n按回车键返回...")
}

func (is *interactiveSession) showHelp() {
	fmt.Println("\n╔════════════════════════════════════╗")
	fmt.Println("║        帮助信息                     ║")
	fmt.Println("╚════════════════════════════════════╝")
	fmt.Println("\n快捷键:")
	fmt.Println("  q, quit, exit - 退出程序")
	fmt.Println("  h, help       - 显示帮助")
	fmt.Println("\n功能说明:")
	fmt.Println("  环境变量管理 - 管理加密的环境变量")
	fmt.Println("  配置文件管理 - 管理加密的配置文件")
	fmt.Println("  Git 操作     - 同步配置到 git 仓库")
	fmt.Println("  会话管理     - 管理登录会话")
	fmt.Println("  查看状态     - 查看系统状态")

	is.prompt("\n按回车键返回...")
}
