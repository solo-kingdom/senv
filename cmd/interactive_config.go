package cmd

import (
	"fmt"
	"strings"

	"github.com/wii/senv/internal/config"
)

// configMenu displays the configuration file management menu
func (is *interactiveSession) configMenu() {
	for {
		fmt.Println("\n┌────────────────────────────────────┐")
		fmt.Println("│  配置文件管理                       │")
		fmt.Println("└────────────────────────────────────┘")
		fmt.Println("1. 查看所有配置文件")
		fmt.Println("2. 按分组查看配置文件")
		fmt.Println("3. 创建配置文件")
		fmt.Println("4. 编辑配置文件")
		fmt.Println("5. 导出配置文件")
		fmt.Println("6. 安装配置 (install)")
		fmt.Println("7. 卸载配置 (uninstall)")
		fmt.Println("8. 删除配置文件")
		fmt.Println("9. 查看配置详情")
		fmt.Println("0. 返回主菜单")

		choice := is.prompt("请选择 [0-9]: ")
		if choice == "" {
			continue
		}

		switch choice {
		case "1":
			is.listConfigs("")
		case "2":
			group := is.prompt("分组名称: ")
			is.listConfigs(group)
		case "3":
			is.createConfig()
		case "4":
			is.editConfig()
		case "5":
			is.exportConfig()
		case "6":
			is.installConfig()
		case "7":
			is.uninstallConfig()
		case "8":
			is.deleteConfig()
		case "9":
			is.getConfig()
		case "0":
			return
		default:
			fmt.Println("❌ 无效选择")
		}
	}
}

func (is *interactiveSession) listConfigs(group string) {
	configs, err := is.configManager.List(group)
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
		fmt.Printf("  %s [%s]\n", cfg.Name, cfg.Group)
		if cfg.Description != "" {
			fmt.Printf("    描述: %s\n", cfg.Description)
		}
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

	group := is.promptWithDefault("分组 (留空为 default)", "")
	description := is.promptWithDefault("描述 (可选)", "")

	if err := is.configManager.Create(name, source, target, group, description); err != nil {
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
	fmt.Printf("分组: %s\n", cfg.Group)
	fmt.Printf("描述: %s\n", cfg.Description)
	fmt.Printf("目标路径: %s\n", cfg.TargetPath)
	fmt.Printf("创建时间: %s\n", cfg.CreatedAt)
	fmt.Printf("更新时间: %s\n", cfg.UpdatedAt)

	is.prompt("\n按回车键继续...")
}

// promptConfigScope asks for an install/uninstall scope: single config,
// group, or all.
func (is *interactiveSession) promptConfigScope() (config.Scope, bool) {
	fmt.Println("范围: 1. 单个配置  2. 单个分组  3. 全部配置")
	choice := is.prompt("请选择 [1-3]: ")
	switch choice {
	case "1":
		name := is.prompt("配置名称: ")
		if name == "" {
			fmt.Println("❌ 配置名称不能为空")
			return config.Scope{}, false
		}
		return config.Scope{Name: name}, true
	case "2":
		group := is.prompt("分组名称: ")
		if group == "" {
			fmt.Println("❌ 分组名称不能为空")
			return config.Scope{}, false
		}
		return config.Scope{Group: group}, true
	case "3":
		return config.Scope{All: true}, true
	default:
		fmt.Println("❌ 无效选择")
		return config.Scope{}, false
	}
}

// installConfig shows the install plan for the chosen scope, then executes it
// after confirmation.
func (is *interactiveSession) installConfig() {
	scope, ok := is.promptConfigScope()
	if !ok {
		return
	}

	plan, err := is.configManager.PlanInstall(scope)
	if err != nil {
		fmt.Printf("❌ 计划失败: %v\n", err)
		return
	}

	fmt.Println("\n安装计划:")
	for _, item := range plan.Items {
		if item.Action == config.ActionError {
			fmt.Printf("  [error] %s: %s\n", item.Name, item.Reason)
		} else {
			fmt.Printf("  [%s] %s -> %s (%s)\n", item.Action, item.Name, item.TargetPath, item.Reason)
		}
	}

	confirm := is.prompt("执行以上安装计划? [y/N]: ")
	if strings.ToLower(confirm) != "y" {
		fmt.Println("已取消")
		return
	}

	if err := is.configManager.ExecuteInstall(plan); err != nil {
		fmt.Printf("❌ 部分安装失败: %v\n", err)
		return
	}
	fmt.Println("✓ 安装完成")
}

// uninstallConfig shows the uninstall plan, then executes it after
// confirmation; locally modified files require per-item confirmation.
func (is *interactiveSession) uninstallConfig() {
	scope, ok := is.promptConfigScope()
	if !ok {
		return
	}

	plan, err := is.configManager.PlanUninstall(scope)
	if err != nil {
		fmt.Printf("❌ 计划失败: %v\n", err)
		return
	}

	fmt.Println("\n卸载计划:")
	for _, item := range plan.Items {
		switch item.Action {
		case config.ActionError:
			fmt.Printf("  [error] %s: %s\n", item.Name, item.Reason)
		case config.ActionChanged:
			fmt.Printf("  [CHANGED] %s -> %s (%s)\n", item.Name, item.TargetPath, item.Reason)
		default:
			fmt.Printf("  [%s] %s -> %s (%s)\n", item.Action, item.Name, item.TargetPath, item.Reason)
		}
	}

	confirm := is.prompt("执行以上卸载计划? [y/N]: ")
	if strings.ToLower(confirm) != "y" {
		fmt.Println("已取消")
		return
	}

	confirmChanged := func(item config.UninstallItem) bool {
		answer := is.prompt(fmt.Sprintf("目标文件已被本地修改，确认删除 %s? [y/N]: ", item.TargetPath))
		return strings.ToLower(answer) == "y"
	}

	if err := is.configManager.ExecuteUninstall(plan, confirmChanged); err != nil {
		fmt.Printf("❌ 部分卸载失败: %v\n", err)
		return
	}
	fmt.Println("✓ 卸载完成")
}
