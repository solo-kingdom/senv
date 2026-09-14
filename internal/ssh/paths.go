package ssh

import (
	"fmt"
	"os"
	"path/filepath"
)

// 分组目录布局（ADR-0023）：~/.ssh/senv/ 下 groups/ 放组片段、keys/ 放落盘
// 私钥，各按组名一级子目录组织；空组（未分组）用保留名 _ungrouped。
const (
	ungroupedGroup = "_ungrouped"
	groupsDirName  = "groups"
	keysDirName    = "keys"
)

// groupDir 把 vault 的组名映射到目录/文件名片段；空组映射为保留名。
func groupDir(group string) string {
	if group == "" {
		return ungroupedGroup
	}
	return group
}

// SenvDir 是 senv 在 ~/.ssh 下的私有根目录（ADR-0001 的 ~/.ssh 选址理由在
// ADR-0023 中保持不变），目录权限 0700。
func SenvDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".ssh", "senv"), nil
}

// groupsDir 是组片段目录：~/.ssh/senv/groups。
func groupsDir() (string, error) {
	root, err := SenvDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, groupsDirName), nil
}

// keysDir 是落盘私钥根目录：~/.ssh/senv/keys。
func keysDir() (string, error) {
	root, err := SenvDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, keysDirName), nil
}

// FragmentPath 返回某组的组片段路径：~/.ssh/senv/groups/<groupDir>.conf。
func FragmentPath(group string) (string, error) {
	dir, err := groupsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, groupDir(group)+".conf"), nil
}

// MaterializePath 是稳定的落盘路径约定（ADR-0023 取代 ADR-0001 的扁平
// 布局）：~/.ssh/senv/keys/<keypair 分组>/<名>，未分组入 _ungrouped。
func MaterializePath(group, name string) (string, error) {
	dir, err := keysDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, groupDir(group), name), nil
}
