package cmd

import (
	"fmt"

	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/storage"
)

// getStorage 是本地工作副本存储（storage.Manager）的统一构造入口。
// 本地加密文件存储始终是工作副本，读写路径与 provider 选择无关。
func getStorage() *storage.Manager {
	return storage.NewManager(getConfigPath(), getDataPath())
}

// getSyncProvider 是远端同步 provider 的统一构造入口：
// 读取 settings.json 的 provider 配置（未配置时默认 git），按类型构造。
// 构造失败时错误信息包含 provider 类型与原因。
func getSyncProvider() (provider.Provider, error) {
	store := getStorage()
	cfg := provider.Config{
		Type:       provider.TypeGit,
		GitPath:    store.GetGitPath(),
		ConfigPath: store.GetConfigPath(),
		DataPath:   store.GetDataPath(),
	}
	// settings 缺失或损坏时保持 git 默认（与现状行为一致）
	if settings, err := store.LoadSettings(); err == nil {
		if settings.Provider.Type != "" {
			cfg.Type = settings.Provider.Type
		}
		cfg.ServerAddress = settings.Provider.Address
		cfg.ServerToken = settings.Provider.Token
		cfg.Vault = settings.Provider.Vault
	}
	p, err := provider.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("构造同步 provider 失败: %w", err)
	}
	return p, nil
}

// getGitProvider 经统一入口构造 provider 并返回 git 适配层实例。
// git 专属管理命令（senv git / 交互 Git 菜单）当前仅支持 git provider。
func getGitProvider() (*provider.GitProvider, error) {
	p, err := getSyncProvider()
	if err != nil {
		return nil, err
	}
	gp, ok := p.(*provider.GitProvider)
	if !ok {
		return nil, fmt.Errorf("当前 provider 不是 git，git 管理命令仅支持 git provider")
	}
	return gp, nil
}
