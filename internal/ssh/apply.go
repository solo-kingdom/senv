package ssh

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wii/senv/internal/storage"
)

// ApplyResult reports what Apply did. Errors collects per-item write failures
// (best-effort semantics); when non-empty Apply returns them joined.
type ApplyResult struct {
	Written        []string // 组片段路径（排序）
	Pruned         []string // 清理的幽灵组片段路径（仅全量）
	Materialized   []string // 本次落盘的 keypair 名（排序）
	KeysSkipped    []string // 已存在而跳过的 keypair 名（排序）
	Registered     bool     // 本次新注册 Include
	IncludeExisted bool     // Include 已存在
	Warnings       []string // 渲染 warning + 未引用私钥 warning + 版本 warning
	Errors         []string
}

// Apply 是 `senv host export` 默认模式的编排：渲染 → 写组片段 →（全量）
// 清幽灵组片段 → 落盘缺失私钥 → 注册 Include。两阶段保证渲染失败
// （如 proxyJump 悬空）时零文件副作用；写盘阶段逐项尽力而为并汇总。
func (m *Manager) Apply(filter RenderFilter) (*ApplyResult, error) {
	res := &ApplyResult{}

	// Host 过滤在应用模式的写入单元是该 host 所在组的整文件（spec
	// "Export single host"），先归一化为组过滤再渲染。
	eff := filter
	if eff.Host != "" {
		host, err := m.loadHost(eff.Host)
		if err != nil {
			return nil, err
		}
		eff = RenderFilter{Group: host.Group}
	}
	full := filter.Host == "" && filter.Group == ""

	rr, err := m.Render(eff)
	if err != nil {
		return nil, err
	}
	res.Warnings = append(res.Warnings, rr.Warnings...)

	gdir, err := groupsDir()
	if err != nil {
		return nil, err
	}
	if err := storage.EnsurePrivateDir(gdir, 0o700); err != nil {
		return nil, err
	}
	for _, group := range rr.Order {
		path, err := FragmentPath(group)
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		if err := storage.WriteSensitiveFile(path, []byte(rr.Fragments[group]), 0o700, 0o600); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("write group fragment %s: %v", path, err))
			continue
		}
		res.Written = append(res.Written, path)
	}

	if full {
		pruned, err := pruneGhostFragments(gdir, fragmentNameSet(rr))
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
		}
		res.Pruned = pruned
	}

	materializeReferenced(rr, res)
	res.Warnings = append(res.Warnings, unreferencedKeyWarnings(rr)...)

	changed, err := RegisterInclude()
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("register include: %v", err))
	} else {
		res.Registered = changed
		res.IncludeExisted = !changed
		if globIncludeUnsupported() {
			res.Warnings = append(res.Warnings, "OpenSSH 低于 7.3，glob Include 不生效；请升级 OpenSSH 或为每组单独添加 Include")
		}
	}

	if len(res.Errors) > 0 {
		return res, fmt.Errorf("export applied with %d error(s):\n%s", len(res.Errors), strings.Join(res.Errors, "\n"))
	}
	return res, nil
}

// Unexport 是导出的撤回（ADR-0023 D7）：摘除 ~/.ssh/config 的 senv 注册
// 行并删除 groups/ 目录；vault 档案与 keys/ 下落盘私钥原样保留。
func (m *Manager) Unexport() (unregistered bool, groupsRemoved bool, err error) {
	var errs []string
	changed, regErr := UnregisterInclude()
	if regErr != nil {
		errs = append(errs, regErr.Error())
	} else {
		unregistered = changed
	}
	gdir, dirErr := groupsDir()
	if dirErr != nil {
		errs = append(errs, dirErr.Error())
	} else if _, statErr := os.Stat(gdir); statErr == nil {
		if rmErr := os.RemoveAll(gdir); rmErr != nil {
			errs = append(errs, fmt.Sprintf("remove group fragments: %v", rmErr))
		} else {
			groupsRemoved = true
		}
	}
	if len(errs) > 0 {
		return unregistered, groupsRemoved, fmt.Errorf("unexport: %s", strings.Join(errs, "; "))
	}
	return unregistered, groupsRemoved, nil
}

// pruneGhostFragments 删除 groups/ 下不属于当前渲染结果的 .conf 文件
// （组已在 vault 中改名/删空）。调用方限定：仅全量导出。
func pruneGhostFragments(gdir string, live map[string]bool) ([]string, error) {
	entries, err := os.ReadDir(gdir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list group fragments: %w", err)
	}
	var pruned []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		if live[entry.Name()] {
			continue
		}
		path := filepath.Join(gdir, entry.Name())
		if err := os.Remove(path); err != nil {
			return pruned, fmt.Errorf("prune stale fragment %s: %w", path, err)
		}
		pruned = append(pruned, path)
	}
	return pruned, nil
}

func fragmentNameSet(rr *RenderResult) map[string]bool {
	names := make(map[string]bool, len(rr.Order))
	for _, group := range rr.Order {
		names[groupDir(group)+".conf"] = true
	}
	return names
}

// materializeReferenced 落盘缺失的被引用私钥；已存在 MUST 跳过不覆盖
// （ADR-0023 D4），force 语义只属于显式 `keypair materialize`。
func materializeReferenced(rr *RenderResult, res *ApplyResult) {
	names := make([]string, 0, len(rr.Referenced))
	for name := range rr.Referenced {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := rr.Referenced[name]
		target, err := MaterializePath(entry.Group, entry.Name)
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		if _, statErr := os.Lstat(target); statErr == nil {
			res.KeysSkipped = append(res.KeysSkipped, name)
			continue
		} else if !errors.Is(statErr, os.ErrNotExist) {
			res.Errors = append(res.Errors, fmt.Sprintf("inspect materialized key %q: %v", target, statErr))
			continue
		}
		if err := storage.EnsurePrivateDir(filepath.Dir(target), 0o700); err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		if err := storage.WriteSensitiveFile(target, []byte(entry.PrivateKey), 0o700, 0o600); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("materialize keypair %q: %v", name, err))
			continue
		}
		res.Materialized = append(res.Materialized, name)
	}
}

// unreferencedKeyWarnings 扫描落盘私钥（含 ADR-0001 遗留的顶层扁平文件），
// 对未被任何 host 引用的逐条 warning——它们可能仍被你手写 ssh config
// 引用，只提示、绝不自动删除（清理是显式 `keypair prune`）。
func unreferencedKeyWarnings(rr *RenderResult) []string {
	referenced := make(map[string]bool, len(rr.Referenced))
	for _, entry := range rr.Referenced {
		path, err := MaterializePath(entry.Group, entry.Name)
		if err != nil {
			continue
		}
		referenced[path] = true
	}
	var paths []string
	root, err := SenvDir()
	if err == nil {
		if entries, err := os.ReadDir(root); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					paths = append(paths, filepath.Join(root, entry.Name()))
				}
			}
		}
	}
	kdir, err := keysDir()
	if err == nil {
		if groups, err := os.ReadDir(kdir); err == nil {
			for _, group := range groups {
				if !group.IsDir() {
					continue
				}
				if files, err := os.ReadDir(filepath.Join(kdir, group.Name())); err == nil {
					for _, file := range files {
						if !file.IsDir() {
							paths = append(paths, filepath.Join(kdir, group.Name(), file.Name()))
						}
					}
				}
			}
		}
	}
	sort.Strings(paths)
	var warnings []string
	for _, path := range paths {
		if !referenced[path] {
			warnings = append(warnings, fmt.Sprintf("落盘私钥 %s 未被任何 host 引用（可用 senv keypair prune 清理）", path))
		}
	}
	return warnings
}
