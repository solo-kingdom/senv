package cmd

import (
	"fmt"
	"strings"
)

// sessionMenu displays the session management menu
func (is *interactiveSession) sessionMenu() {
	for {
		fmt.Println("\n┌────────────────────────────────────┐")
		fmt.Println("│  会话管理                           │")
		fmt.Println("└────────────────────────────────────┘")

		cache, _ := is.sessionManager.LoadCache()
		if cache != nil {
			valid, _ := is.sessionManager.IsCacheValid(cache)
			if valid {
				fmt.Println("状态: 已登录")
			} else {
				fmt.Println("状态: 会话已过期")
			}
		} else {
			fmt.Println("状态: 未登录")
		}

		fmt.Println("\n1. 查看会话状态")
		fmt.Println("2. 清除会话")
		fmt.Println("0. 返回主菜单")

		choice := is.prompt("请选择 [0-2]: ")
		if choice == "" {
			continue
		}

		switch choice {
		case "1":
			is.showSessionStatus()
		case "2":
			is.clearSession()
		case "0":
			return
		default:
			fmt.Println("❌ 无效选择")
		}
	}
}

func (is *interactiveSession) showSessionStatus() {
	cache, err := is.sessionManager.LoadCache()
	if err != nil {
		fmt.Printf("\n❌ 加载会话失败: %v\n", err)
		return
	}

	if cache == nil {
		fmt.Println("\n会话状态: 未登录")
		return
	}

	valid, err := is.sessionManager.IsCacheValid(cache)
	if err != nil {
		fmt.Printf("\n会话状态: 无效 (%v)\n", err)
		return
	}

	if !valid {
		fmt.Println("\n会话状态: 已过期")
		fmt.Printf("会话 ID: %s\n", cache.SessionID)
		fmt.Printf("创建时间: %s\n", cache.CreatedAt.Format("2006-01-02 15:04:05"))
		return
	}

	fmt.Println("\n会话状态: 已登录")
	fmt.Printf("会话 ID: %s\n", cache.SessionID)
	fmt.Printf("创建时间: %s\n", cache.CreatedAt.Format("2006-01-02 15:04:05"))

	is.prompt("\n按回车键继续...")
}

func (is *interactiveSession) clearSession() {
	confirm := is.prompt("确认清除会话? [y/N]: ")
	if strings.ToLower(confirm) != "y" {
		fmt.Println("已取消")
		return
	}

	if err := is.sessionManager.ClearSession(); err != nil {
		fmt.Printf("❌ 清除失败: %v\n", err)
		return
	}

	fmt.Println("✓ 会话已清除")
}
