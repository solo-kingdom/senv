package cmd

import (
	"fmt"
	"strings"
)

// configMenu displays the configuration file management menu
func (is *interactiveSession) configMenu() {
	for {
		fmt.Println("\n┌────────────────────────────────────┐")
		fmt.Println("│  配置文件管理                       │")
		fmt.Println("└────────────────────────────────────┘")
		fmt.Println("1. 查看所有配置文件")
		fmt.Println("2. 创建配置文件")
		fmt.Println("3. 编辑配置文件")
		fmt.Println("4. 导出配置文件")
		fmt.Println("5. 删除配置文件")
		fmt.Println("6. 查看配置详情")
		fmt.Println("0. 返回主菜单")

		choice := is.prompt("请选择 [0-6]: ")
		if choice == "" {
			continue
		}

		switch choice {
		case "1":
			is.listConfigs()
		case "2":
			is.createConfig()
		case "3":
			is.editConfig()
		case "4":
			is.exportConfig()
		case "5":
			is.deleteConfig()
		case "6":
			is.getConfig()
		case "0":
			return
		default:
			fmt.Println("❌ 无效选择")
		}
	}
}

func (is *interactiveSession) listConfigs() {
	configs, err := is.configManager.List()
	if err != nil {
		fmt.Printf("❌ 错误: %v\n", err)
		return
	}

	if len(configs) == 0 {
		fmt.Println("\n没有找到配置文件")
		return
	}

	fmt.Println("\n配置文件:")
	for _, cfg := range configs {
		fmt.Printf("  %s\n", cfg.Name)
		fmt.Printf("    目标路径: %s\n", cfg.TargetPath)
		fmt.Printf("    更新时间: %s\n", cfg.UpdatedAt)
	}

	is.prompt("\n按回车键继续...")
}

func (is *interactiveSession) createConfig() {
	name := is.prompt("配置名称: ")
	if name == "" {
		fmt.Println("❌ 配置名称不能为空")
		return
	}

	source := is.prompt("源文件路径: ")
	if source == "" {
		fmt.Println("❌ 源文件路径不能为空")
		return
	}

	target := is.prompt("目标路径: ")
	if target == "" {
		fmt.Println("❌ 目标路径不能为空")
		return
	}

	if err := is.configManager.Create(name, source, target); err != nil {
		fmt.Printf("❌ 创建失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 已创建配置: %s\n", name)
}

func (is *interactiveSession) editConfig() {
	name := is.prompt("配置名称: ")
	if name == "" {
		fmt.Println("❌ 配置名称不能为空")
		return
	}

	fmt.Println("正在打开编辑器...")
	if err := is.configManager.Edit(name); err != nil {
		fmt.Printf("❌ 编辑失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 已保存配置: %s\n", name)
}

func (is *interactiveSession) exportConfig() {
	name := is.prompt("配置名称: ")
	if name == "" {
		fmt.Println("❌ 配置名称不能为空")
		return
	}

	target := is.promptWithDefault("目标路径 (留空使用默认)", "")

	if err := is.configManager.Export(name, target); err != nil {
		fmt.Printf("❌ 导出失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 已导出配置: %s\n", name)
}

func (is *interactiveSession) deleteConfig() {
	name := is.prompt("配置名称: ")
	if name == "" {
		fmt.Println("❌ 配置名称不能为空")
		return
	}

	confirm := is.prompt(fmt.Sprintf("确认删除配置 %s? [y/N]: ", name))
	if strings.ToLower(confirm) != "y" {
		fmt.Println("已取消")
		return
	}

	if err := is.configManager.Delete(name); err != nil {
		fmt.Printf("❌ 删除失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 已删除配置: %s\n", name)
}

func (is *interactiveSession) getConfig() {
	name := is.prompt("配置名称: ")
	if name == "" {
		fmt.Println("❌ 配置名称不能为空")
		return
	}

	cfg, err := is.configManager.Get(name)
	if err != nil {
		fmt.Printf("❌ 错误: %v\n", err)
		return
	}

	fmt.Printf("\n名称: %s\n", cfg.Name)
	fmt.Printf("目标路径: %s\n", cfg.TargetPath)
	fmt.Printf("创建时间: %s\n", cfg.CreatedAt)
	fmt.Printf("更新时间: %s\n", cfg.UpdatedAt)

	is.prompt("\n按回车键继续...")
}
