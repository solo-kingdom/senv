package ssh

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PruneCandidate 是一个未被任何 host 引用的落盘私钥文件。
type PruneCandidate struct {
	Path    string
	Group   string // 磁盘分组目录（_ungrouped 或组名；顶层遗留扁平文件为 ""）
	Name    string
	InVault bool // 对应 keypair 是否仍在 vault 中（改组后的旧路径文件为 true）
}

// PruneCandidates 列出 ~/.ssh/senv/keys/<组>/<名> 及顶层遗留扁平文件中，
// 未被任何 vault host 的 identityKey 引用的私钥文件。"未引用"按当前
// (分组, 名) 判定：keypair 改组后旧路径文件自然落入清单。
func (m *Manager) PruneCandidates() ([]PruneCandidate, error) {
	hosts, err := m.ListHosts()
	if err != nil {
		return nil, err
	}
	referenced := make(map[string]bool)
	for _, host := range hosts {
		if host.IdentityKey == "" {
			continue
		}
		entry, err := m.loadKeyPair(host.IdentityKey)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		path, err := MaterializePath(entry.Group, entry.Name)
		if err != nil {
			return nil, err
		}
		referenced[path] = true
	}

	var candidates []PruneCandidate
	root, err := SenvDir()
	if err != nil {
		return nil, err
	}
	// 顶层遗留扁平文件（ADR-0001 老布局残留）。
	if entries, err := os.ReadDir(root); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(root, entry.Name())
			if referenced[path] {
				continue
			}
			candidates = append(candidates, PruneCandidate{Path: path, Name: entry.Name()})
		}
	}
	kdir, err := keysDir()
	if err != nil {
		return nil, err
	}
	if groups, err := os.ReadDir(kdir); err == nil {
		for _, group := range groups {
			if !group.IsDir() {
				continue
			}
			gdir := filepath.Join(kdir, group.Name())
			files, err := os.ReadDir(gdir)
			if err != nil {
				return nil, err
			}
			for _, file := range files {
				if file.IsDir() {
					continue
				}
				path := filepath.Join(gdir, file.Name())
				if referenced[path] {
					continue
				}
				candidates = append(candidates, PruneCandidate{
					Path:  path,
					Group: group.Name(),
					Name:  file.Name(),
				})
			}
		}
	}
	for i := range candidates {
		_, statErr := m.loadKeyPair(candidates[i].Name)
		candidates[i].InVault = statErr == nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Path < candidates[j].Path })
	return candidates, nil
}

// DeletePrunedFiles 删除给定路径的落盘私钥文件，返回实际删除列表。
func DeletePrunedFiles(paths []string) ([]string, error) {
	var deleted []string
	var errs []string
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			errs = append(errs, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		deleted = append(deleted, path)
	}
	if len(errs) > 0 {
		return deleted, fmt.Errorf("prune: %s", strings.Join(errs, "; "))
	}
	return deleted, nil
}
