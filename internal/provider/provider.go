// Package provider 定义 senv 远端同步通道的窄抽象。
//
// 本地加密文件存储（internal/storage）始终是工作副本，provider 只负责 push/pull 同步语义：
// git remote 与 senv-server 是对等的两种 remote，provider 不抽象 storage 的本地读写方法。
package provider

import (
	"fmt"
	"strings"

	"github.com/wii/senv/internal/git"
)

const (
	// TypeGit 本地文件存储 + git remote 同步（默认）
	TypeGit = "git"
	// TypeServer 本地文件存储 + senv-server 同步（后续子 change 实现）
	TypeServer = "server"
)

// Config 远端 provider 配置，来源于 settings.json 的 provider 字段
type Config struct {
	Type string // 空或 "git" → git provider；"server" → senv-server
	// GitPath 是 git 仓库根目录（git provider 使用）
	GitPath string
	// ServerAddress / ServerToken 为 server provider 必填项
	ServerAddress string
	ServerToken   string
	// ConfigPath / DataPath 为 server 模式本地缓存目录（复用 storage.Manager 文件格式）
	ConfigPath string
	DataPath   string
	// Vault 为 server 端 vault 名，默认 "main"
	Vault string
}

// Provider 是围绕 push/pull 语义的窄同步通道接口
type Provider interface {
	// Push 提交并推送本地变更到远端
	Push(message string) error
	// Pull 从远端拉取变更
	Pull() error
	// Sync 双向同步：提交本地变更 → 拉取 → 推送
	Sync(message string) error
	// Status 返回本地工作副本相对远端的状态描述
	Status() (string, error)
}

// New 是 provider 的统一构造入口，按配置类型选择具体实现
func New(cfg Config) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "", TypeGit:
		if cfg.GitPath == "" {
			return nil, fmt.Errorf("provider %s: git 仓库路径为空", TypeGit)
		}
		return NewGitProvider(git.NewManager(cfg.GitPath)), nil
	case TypeServer:
		var missing []string
		if strings.TrimSpace(cfg.ServerAddress) == "" {
			missing = append(missing, "address")
		}
		if strings.TrimSpace(cfg.ServerToken) == "" {
			missing = append(missing, "token")
		}
		// server provider 配置校验：缺参时给出明确错误，绝不静默回退 git
		if len(missing) > 0 {
			return nil, fmt.Errorf("provider server: 配置不完整，缺少: %s", strings.Join(missing, ", "))
		}
		vault := cfg.Vault
		if vault == "" {
			vault = "main"
		}
		return NewServerProvider(cfg.ServerAddress, cfg.ServerToken, cfg.ConfigPath, cfg.DataPath, vault), nil
	default:
		return nil, fmt.Errorf("provider %q: 未知类型，支持 %s / %s", cfg.Type, TypeGit, TypeServer)
	}
}
