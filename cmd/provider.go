package cmd

import (
	"fmt"
	"sync"

	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/storage"
)

// getStorage 是本地工作副本存储（storage.Manager）的统一构造入口。
// 本地加密文件存储始终是工作副本，读写路径与 provider 选择无关。
func getStorage() *storage.Manager {
	return storage.NewManager(getConfigPath(), getDataPath())
}

// providerCacheKey 是 provider 单例缓存的归一化键：Config.AutoSync 是指针，
// 每次读 settings 都是新指针，不能直接作 map 键，故按值展开。
type providerCacheKey struct {
	providerType string
	gitPath      string
	serverAddr   string
	serverToken  string
	configPath   string
	dataPath     string
	vault        string
	autoSyncSet  bool
	autoSync     bool
	syncThrottle string
}

// providerCache 实现「同一进程内同一份配置复用同一 provider 实例」：TUI 的
// pull/push/History 与 CLI 的 autoPull/AutoPush 因此共享同一 HTTP 连接池，
// 不再各付一次 TLS 建连。settings 在进程生命周期内视为不变（与现状一致：
// CLI 每命令一进程；TUI/MCP 长驻进程改配置需重启）。
var (
	providerMu    sync.Mutex
	providerCache = map[providerCacheKey]provider.Provider{}
)

// getSyncProvider 是远端同步 provider 的统一构造入口：
// 读取 settings.json 的 provider 配置（未配置时默认 git），按类型构造。
// 构造失败时错误信息包含 provider 类型与原因；失败不缓存，下次调用重试。
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
		cfg.AutoSync = settings.Provider.AutoSync
		cfg.SyncThrottle = settings.Provider.SyncThrottle
	}

	key := providerCacheKey{
		providerType: cfg.Type,
		gitPath:      cfg.GitPath,
		serverAddr:   cfg.ServerAddress,
		serverToken:  cfg.ServerToken,
		configPath:   cfg.ConfigPath,
		dataPath:     cfg.DataPath,
		vault:        cfg.Vault,
		syncThrottle: cfg.SyncThrottle,
	}
	if cfg.AutoSync != nil {
		key.autoSyncSet = true
		key.autoSync = *cfg.AutoSync
	}

	providerMu.Lock()
	defer providerMu.Unlock()
	if p, ok := providerCache[key]; ok {
		return p, nil
	}
	p, err := provider.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("构造同步 provider 失败: %w", err)
	}
	providerCache[key] = p
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
