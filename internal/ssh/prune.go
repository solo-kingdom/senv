package ssh

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wii/senv/internal/securefs"
)

// PruneCandidate 是一个未被任何 host 引用的落盘私钥文件。
type PruneCandidate struct {
	Path    string
	Group   string // 磁盘分组目录（_ungrouped 或组名；顶层遗留扁平文件为 ""）
	Name    string
	InVault bool // 对应 keypair 是否仍在 vault 中（改组后的旧路径文件为 true）
}

// PruneCandidates 列出 ~/.ssh/senv/keys/<组>/<名> 及顶层遗留扁平文件中，
// 未被任何 vault host 的 identityKey 引用、也不是本机默认 IdentityFile
// 的私钥文件（含伴生 .pub）。"未引用"按当前 (分组, 名) 判定：keypair
// 改组后旧路径文件自然落入清单；被引用私钥的 .pub 与私钥一同视为引用。
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
		markMaterializedPaths(referenced, path)
	}
	if identity, err := DefaultIdentityFile(); err != nil {
		return nil, err
	} else if identity != "" {
		markMaterializedPaths(referenced, identity)
	}

	var candidates []PruneCandidate
	root, err := SenvDir()
	if err != nil {
		return nil, err
	}
	senvRoot, err := securefs.OpenRoot(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open senv ssh root: %w", err)
	}
	defer senvRoot.Close()

	// 顶层遗留扁平文件（ADR-0001 老布局残留）。
	if entries, err := senvRoot.ReadDir(); err == nil {
		for _, entry := range entries {
			if entry.IsDir {
				continue
			}
			path := filepath.Join(root, entry.Name)
			if referenced[path] {
				continue
			}
			candidates = append(candidates, PruneCandidate{Path: path, Name: entry.Name})
		}
	}
	kdir, err := keysDir()
	if err != nil {
		return nil, err
	}
	if groups, err := senvRoot.ReadDir(keysDirName); err == nil {
		for _, group := range groups {
			if !group.IsDir {
				continue
			}
			gdir := filepath.Join(kdir, group.Name)
			files, err := senvRoot.ReadDir(keysDirName, group.Name)
			if err != nil {
				return nil, err
			}
			for _, file := range files {
				if file.IsDir {
					continue
				}
				path := filepath.Join(gdir, file.Name)
				if referenced[path] {
					continue
				}
				candidates = append(candidates, PruneCandidate{
					Path:  path,
					Group: group.Name,
					Name:  file.Name,
				})
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for i := range candidates {
		_, statErr := m.loadKeyPair(vaultNameForMaterializedFile(candidates[i].Name))
		candidates[i].InVault = statErr == nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Path < candidates[j].Path })
	return candidates, nil
}

// DeletePrunedFiles 删除给定路径的落盘私钥文件（必须严格位于 ~/.ssh/senv 下的
// 顶层遗留文件或 keys/<组>/<名>），不跟随任何软链，返回实际删除列表。
func DeletePrunedFiles(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	root, err := SenvDir()
	if err != nil {
		return nil, err
	}
	cleanRoot := filepath.Clean(root)
	senvRoot, err := securefs.OpenRoot(cleanRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open senv ssh root: %w", err)
	}
	defer senvRoot.Close()

	var deleted []string
	var errs []string
	for _, path := range paths {
		segs, relErr := prunedRelSegments(cleanRoot, path)
		if relErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", path, relErr))
			continue
		}
		if err := senvRoot.Remove(segs...); err != nil {
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

func prunedRelSegments(cleanRoot, path string) ([]string, error) {
	rel, err := filepath.Rel(cleanRoot, filepath.Clean(path))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("path is outside %s", cleanRoot)
	}
	segs := strings.Split(filepath.ToSlash(rel), "/")
	for _, s := range segs {
		if err := securefs.ValidateSegment(s); err != nil {
			return nil, fmt.Errorf("invalid segment %q: %w", s, err)
		}
	}
	switch {
	case len(segs) == 1:
		return segs, nil
	case len(segs) == 3 && segs[0] == keysDirName:
		return segs, nil
	default:
		return nil, fmt.Errorf("unexpected prune target path depth")
	}
}
