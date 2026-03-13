package cmd

import (
	"fmt"
	"time"
)

// gitMenu displays the git operations menu
func (is *interactiveSession) gitMenu() {
	if !is.gitManager.IsGitRepo() {
		fmt.Println("\n❌ 数据路径不是 git 仓库")
		fmt.Printf("数据路径: %s\n", is.storageManager.GetDataPath())
		fmt.Println("\n要使用 git 功能，请先初始化 git 仓库:")
		fmt.Printf("  cd %s\n", is.storageManager.GetDataPath())
		fmt.Println("  git init")
		fmt.Println("  git remote add origin <repository-url>")
		is.prompt("\n按回车键返回...")
		return
	}

	if !is.gitManager.IsGitRoot() {
		fmt.Println("\n❌ 数据路径不是 git 仓库的根目录")
		fmt.Println("请使用 git 仓库的根目录作为数据路径")
		is.prompt("\n按回车键返回...")
		return
	}

	for {
		fmt.Println("\n┌────────────────────────────────────┐")
		fmt.Println("│  Git 操作                           │")
		fmt.Println("└────────────────────────────────────┘")

		info, err := is.gitManager.GetStatusInfo()
		if err == nil {
			fmt.Printf("当前分支: %s\n", info.CurrentBranch)
			if info.HasChanges {
				fmt.Println("状态: 有待提交的更改")
			} else {
				fmt.Println("状态: 工作目录干净")
			}
		}

		fmt.Println("\n1. 拉取更新 (pull)")
		fmt.Println("2. 提交更改 (commit)")
		fmt.Println("3. 推送更改 (push)")
		fmt.Println("4. 同步更改 (add + commit + push)")
		fmt.Println("5. 查看详细状态")
		fmt.Println("0. 返回主菜单")

		choice := is.prompt("请选择 [0-5]: ")
		if choice == "" {
			continue
		}

		switch choice {
		case "1":
			is.gitPull()
		case "2":
			is.gitCommit()
		case "3":
			is.gitPush()
		case "4":
			is.gitSync()
		case "5":
			is.gitStatus()
		case "0":
			return
		default:
			fmt.Println("❌ 无效选择")
		}
	}
}

func (is *interactiveSession) gitPull() {
	fmt.Println("\n正在拉取更新...")
	if err := is.gitManager.Pull(); err != nil {
		fmt.Printf("❌ 拉取失败: %v\n", err)
		return
	}
	fmt.Println("✓ 成功拉取更新")
}

func (is *interactiveSession) gitCommit() {
	message := is.prompt("提交信息: ")
	if message == "" {
		fmt.Println("❌ 提交信息不能为空")
		return
	}

	fmt.Println("正在提交...")
	if err := is.gitManager.Add(); err != nil {
		fmt.Printf("❌ 添加更改失败: %v\n", err)
		return
	}

	if err := is.gitManager.Commit(message); err != nil {
		fmt.Printf("❌ 提交失败: %v\n", err)
		return
	}

	fmt.Println("✓ 成功提交更改")
}

func (is *interactiveSession) gitPush() {
	fmt.Println("\n正在推送...")
	if err := is.gitManager.Push(); err != nil {
		fmt.Printf("❌ 推送失败: %v\n", err)
		return
	}
	fmt.Println("✓ 成功推送更改")
}

func (is *interactiveSession) gitSync() {
	message := is.prompt("提交信息: ")
	if message == "" {
		message = fmt.Sprintf("Update configurations - %s", time.Now().Format("2006-01-02 15:04:05"))
	}

	fmt.Println("\n正在同步...")
	if err := is.gitManager.AddCommitPush(message); err != nil {
		fmt.Printf("❌ 同步失败: %v\n", err)
		return
	}
	fmt.Println("✓ 成功同步更改")
}

func (is *interactiveSession) gitStatus() {
	info, err := is.gitManager.GetStatusInfo()
	if err != nil {
		fmt.Printf("❌ 错误: %v\n", err)
		return
	}

	fmt.Printf("\n数据路径: %s\n", info.Path)
	fmt.Printf("是否为 git 仓库: %v\n", info.IsGitRepo)
	fmt.Printf("是否为根目录: %v\n", info.IsGitRoot)

	if info.IsGitRepo {
		fmt.Printf("当前分支: %s\n", info.CurrentBranch)
		if info.RemoteURL != "" {
			fmt.Printf("远程仓库: %s\n", info.RemoteURL)
		}

		if info.HasChanges {
			fmt.Println("待提交更改: 是")

			status, err := is.gitManager.Status()
			if err == nil && status != "" {
				fmt.Println("\n更改详情:")
				fmt.Print(status)
			}
		} else {
			fmt.Println("待提交更改: 否")
		}
	}

	is.prompt("\n按回车键继续...")
}
