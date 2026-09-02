package provider

import "github.com/wii/senv/internal/git"

// GitProvider 是 git provider 的薄适配层：委托现有 internal/git.Manager，
// 不重复包装底层错误，行为与直连 git.Manager 完全一致。
type GitProvider struct {
	manager *git.Manager
}

// NewGitProvider 创建 git provider 适配层实例
func NewGitProvider(manager *git.Manager) *GitProvider {
	return &GitProvider{manager: manager}
}

// Push 提交并推送本地变更到远端
func (p *GitProvider) Push(message string) error {
	return p.manager.AddCommitPush(message)
}

// Pull 从远端拉取变更
func (p *GitProvider) Pull() error {
	return p.manager.Pull()
}

// Sync 双向同步：提交本地变更 → pull --rebase → push
func (p *GitProvider) Sync(message string) error {
	return p.manager.Sync(message)
}

// Status 返回本地工作副本相对远端的状态描述
func (p *GitProvider) Status() (string, error) {
	return p.manager.Status()
}

// Manager 返回底层 git.Manager，供 git 专属管理命令与交互菜单使用
func (p *GitProvider) Manager() *git.Manager {
	return p.manager
}
