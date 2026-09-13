// server-token：server provider 凭据的机器本地存储。
//
// token 是 vault 密文的完整读取凭证。它绝不能进入任何会被 git provider
// 提交的文件：git 同步是仓库根的 `git add .`（见 internal/git），settings.json
// 在默认路径布局下位于仓库根内。因此 token 单独存放在 server-token.json，
// 并由 EnsureGitIgnoreServerToken / git add 排除路径双重保证不入库。
package storage

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ServerTokenFile 是 server provider token 的机器本地存储文件（0600）。
const ServerTokenFile = "server-token.json"

// GitIgnoreFile 是写入 configPath 的 .gitignore 文件名。
const GitIgnoreFile = ".gitignore"

// gitIgnoredMachineLocal 列出必须被 git 同步排除的机器本地文件。
// mcp-exports.json 含各 agent 导出配置的指纹（对低熵 secret 是离线验证
// 预言机），同样只在单机有意义。
var gitIgnoredMachineLocal = []string{ServerTokenFile, "mcp-exports.json"}

// ErrServerTokenNotFound 表示 token 文件不存在（未注册/未配置 server provider）
var ErrServerTokenNotFound = errors.New("server token file not found")

// LoadServerToken 读取机器本地 token；文件不存在时返回 ErrServerTokenNotFound。
func (m *Manager) LoadServerToken() (string, error) {
	root, err := m.openConfigRoot()
	if err != nil {
		return "", err
	}
	defer root.Close()
	data, err := root.Read(ServerTokenFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrServerTokenNotFound
		}
		return "", err
	}
	// 容忍手写文件：解析 JSON，缺字段视为空
	var payload struct {
		Token string `json:"token"`
	}
	if err := FromJSON(data, &payload); err != nil {
		return "", fmt.Errorf("解析 %s 失败: %w", ServerTokenFile, err)
	}
	if strings.TrimSpace(payload.Token) == "" {
		return "", fmt.Errorf("%s 中没有 token 字段", ServerTokenFile)
	}
	return payload.Token, nil
}

// SaveServerToken 以 0600 原子写保存 token，并确保 .gitignore 覆盖该文件。
func (m *Manager) SaveServerToken(token string) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("token 不能为空")
	}
	if err := m.EnsureGitIgnoreServerToken(); err != nil {
		return err
	}
	data, err := ToJSON(struct {
		Token string `json:"token"`
	}{Token: token})
	if err != nil {
		return err
	}
	root, err := m.openConfigRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.AtomicWrite([]string{ServerTokenFile}, data, 0o600)
}

// ClearServerToken 删除机器本地 token 文件（文件不存在视为成功）。
func (m *Manager) ClearServerToken() error {
	root, err := m.openConfigRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(ServerTokenFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// EnsureGitIgnoreServerToken 确保 configPath 下存在 .gitignore 且覆盖全部
// 机器本地敏感文件。已存在的条目不重复；其余既有内容原样保留。
// configPath 不在任何 git 仓库内时该文件无害。
func (m *Manager) EnsureGitIgnoreServerToken() error {
	root, err := m.openConfigRoot()
	if err != nil {
		return err
	}
	defer root.Close()

	existing := ""
	if data, err := root.Read(GitIgnoreFile); err == nil {
		existing = string(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	updated := existing
	if updated != "" && !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	for _, name := range gitIgnoredMachineLocal {
		if ignoreLineCovers(existing, name) {
			continue
		}
		updated += name + "\n"
	}
	if updated == existing {
		return nil
	}
	return root.AtomicWrite([]string{GitIgnoreFile}, []byte(updated), 0o600)
}

// ignoreLineCovers 判断 .gitignore 内容是否已有覆盖 name 的规则
// （精确行、`/name` 锚定或 `name/` 目录形式均接受；不支持通配语义）。
func ignoreLineCovers(content, name string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		switch line {
		case name, "/" + name, name + "/":
			return true
		}
	}
	return false
}

// MigrateServerTokenFromSettings 把遗留在 settings.json provider 配置里的
// token 搬到机器本地文件并从 settings 中清除（一次性迁移，幂等）。
// 返回是否发生了迁移。该函数供命令层在读 token 前调用；迁移失败时保持
// 原状并返回错误，调用方可回退到 settings 内 token 继续工作。
func (m *Manager) MigrateServerTokenFromSettings() (bool, error) {
	settings, err := m.LoadSettings()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if strings.TrimSpace(settings.Provider.Token) == "" {
		return false, nil
	}
	if err := m.SaveServerToken(settings.Provider.Token); err != nil {
		return false, err
	}
	settings.Provider.Token = ""
	if err := m.SaveSettings(settings); err != nil {
		return false, fmt.Errorf("token 已写入 %s 但清理 settings.json 失败: %w", ServerTokenFile, err)
	}
	return true, nil
}
