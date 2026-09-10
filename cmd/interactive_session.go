package cmd

import (
	"fmt"
	"strings"

	"github.com/wii/senv/internal/session"
)

func sessionStateText(state session.SessionState) string {
	switch state {
	case session.StateActive:
		return "已登录"
	case session.StateExpired:
		return "会话已到期"
	case session.StateInvalidated:
		return "会话已失效"
	case session.StateUnverifiable:
		return "会话不可判定（缓存保留）"
	default:
		return "未登录"
	}
}

// sessionMenu displays the session management menu
func (is *interactiveSession) sessionMenu() {
	for {
		fmt.Println("\n┌────────────────────────────────────┐")
		fmt.Println("│  会话管理                           │")
		fmt.Println("└────────────────────────────────────┘")

		fmt.Printf("状态: %s\n", sessionStateText(is.sessionManager.DescribeCache().State))

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
	status := is.sessionManager.DescribeCache()
	fmt.Printf("\n会话状态: %s\n", sessionStateText(status.State))
	if status.Cache != nil {
		fmt.Printf("会话 ID: %s\n", status.Cache.SessionID)
		fmt.Printf("创建时间: %s\n", status.Cache.CreatedAt.Format("2006-01-02 15:04:05"))
	}
	if status.Reason != session.ReasonNone {
		fmt.Printf("原因: %s\n", sessionReasonText(status.Reason))
	}
	if status.State == session.StateUnverifiable {
		fmt.Println("缓存保留: 是")
	}

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
