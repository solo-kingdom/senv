package cmd

import (
	"fmt"
	"strings"
)

// envMenu displays the environment variable management menu
func (is *interactiveSession) envMenu() {
	for {
		fmt.Println("\n┌────────────────────────────────────┐")
		fmt.Println("│  环境变量管理                       │")
		fmt.Println("└────────────────────────────────────┘")
		fmt.Println("1. 查看所有环境变量")
		fmt.Println("2. 设置环境变量")
		fmt.Println("3. 删除环境变量")
		fmt.Println("4. 管理分组")
		fmt.Println("5. 导出环境变量")
		fmt.Println("0. 返回主菜单")

		choice := is.prompt("请选择 [0-5]: ")
		if choice == "" {
			continue
		}

		switch choice {
		case "1":
			is.listEnvVars()
		case "2":
			is.setEnvVar()
		case "3":
			is.deleteEnvVar()
		case "4":
			is.envGroupMenu()
		case "5":
			is.exportEnv()
		case "0":
			return
		default:
			fmt.Println("❌ 无效选择")
		}
	}
}

// envGroupMenu displays the environment variable group management menu
func (is *interactiveSession) envGroupMenu() {
	for {
		fmt.Println("\n┌────────────────────────────────────┐")
		fmt.Println("│  环境变量分组管理                   │")
		fmt.Println("└────────────────────────────────────┘")
		fmt.Println("1. 查看所有分组")
		fmt.Println("2. 创建新分组")
		fmt.Println("3. 激活分组")
		fmt.Println("4. 停用分组")
		fmt.Println("0. 返回上级菜单")

		choice := is.prompt("请选择 [0-4]: ")
		if choice == "" {
			continue
		}

		switch choice {
		case "1":
			is.listEnvGroups()
		case "2":
			is.addEnvGroup()
		case "3":
			is.activateEnvGroup()
		case "4":
			is.deactivateEnvGroup()
		case "0":
			return
		default:
			fmt.Println("❌ 无效选择")
		}
	}
}

func (is *interactiveSession) listEnvVars() {
	group := is.promptWithDefault("分组名称", "default")
	vars, err := is.envManager.List(group)
	if err != nil {
		fmt.Printf("❌ 错误: %v\n", err)
		return
	}

	if len(vars) == 0 {
		fmt.Println("\n没有找到环境变量")
		return
	}

	for groupName, variables := range vars {
		fmt.Printf("\n[%s]\n", groupName)
		if len(variables) == 0 {
			fmt.Println("  (空)")
			continue
		}
		for key, value := range variables {
			displayValue := value
			if len(value) > 50 {
				displayValue = value[:47] + "..."
			}
			fmt.Printf("  %s=%s\n", key, displayValue)
		}
	}

	is.prompt("\n按回车键继续...")
}

func (is *interactiveSession) setEnvVar() {
	group := is.promptWithDefault("分组名称", "default")
	key := is.prompt("变量名: ")
	if key == "" {
		fmt.Println("❌ 变量名不能为空")
		return
	}

	value := is.prompt("变量值: ")
	if err := is.envManager.Set(group, key, value); err != nil {
		fmt.Printf("❌ 设置失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 已设置 %s=%s (分组: %s)\n", key, value, group)
}

func (is *interactiveSession) deleteEnvVar() {
	group := is.promptWithDefault("分组名称", "default")
	key := is.prompt("变量名: ")
	if key == "" {
		fmt.Println("❌ 变量名不能为空")
		return
	}

	confirm := is.prompt(fmt.Sprintf("确认删除 %s? [y/N]: ", key))
	if strings.ToLower(confirm) != "y" {
		fmt.Println("已取消")
		return
	}

	if err := is.envManager.Delete(group, key); err != nil {
		fmt.Printf("❌ 删除失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 已删除 %s (分组: %s)\n", key, group)
}

func (is *interactiveSession) listEnvGroups() {
	groups, err := is.envManager.ListGroups()
	if err != nil {
		fmt.Printf("❌ 错误: %v\n", err)
		return
	}

	if len(groups) == 0 {
		fmt.Println("\n没有找到分组")
		return
	}

	fmt.Println("\n环境变量分组:")
	for _, group := range groups {
		status := "未激活"
		if group.IsActive {
			status = "已激活"
		}
		defaultMark := ""
		if group.IsDefault {
			defaultMark = " (默认)"
		}
		fmt.Printf("  %s%s - %s - %d 个变量\n", group.Name, defaultMark, status, group.VarCount)
	}

	is.prompt("\n按回车键继续...")
}

func (is *interactiveSession) addEnvGroup() {
	name := is.prompt("新分组名称: ")
	if name == "" {
		fmt.Println("❌ 分组名称不能为空")
		return
	}

	if err := is.envManager.AddGroup(name); err != nil {
		fmt.Printf("❌ 创建失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 已创建分组: %s\n", name)
}

func (is *interactiveSession) activateEnvGroup() {
	name := is.prompt("分组名称: ")
	if name == "" {
		fmt.Println("❌ 分组名称不能为空")
		return
	}

	if err := is.envManager.ActivateGroup(name); err != nil {
		fmt.Printf("❌ 激活失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 已激活分组: %s\n", name)
}

func (is *interactiveSession) deactivateEnvGroup() {
	name := is.prompt("分组名称: ")
	if name == "" {
		fmt.Println("❌ 分组名称不能为空")
		return
	}

	if err := is.envManager.DeactivateGroup(name); err != nil {
		fmt.Printf("❌ 停用失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 已停用分组: %s\n", name)
}

func (is *interactiveSession) exportEnv() {
	exports, err := is.envManager.Export()
	if err != nil {
		fmt.Printf("❌ 导出失败: %v\n", err)
		return
	}

	if exports == "" {
		fmt.Println("\n没有可导出的环境变量")
		return
	}

	fmt.Println("\n导出命令:")
	fmt.Println(exports)
	fmt.Println("\n提示: 在 shell 中运行 'eval $(senv env export)' 来应用这些环境变量")

	is.prompt("\n按回车键继续...")
}
