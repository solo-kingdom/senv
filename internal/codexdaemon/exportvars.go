package codexdaemon

import (
	"fmt"
	"sort"

	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/ref"
)

// ExportVars 把「`senv env export` 会打印的变量」解析为 map，供重启子进程注入。
//
// 它与 cmd 的 resolveExportShell 走同一条链路：取激活组的变量，再按组内隐式
// 组解析 {{env:...}} / {{text:...}} 引用。缺失引用保留模板原文（松散语义），
// 与 export 命令一致——某个跨机未同步的条目不该让整个重启失败。这类 warning
// 一并与变量返回，调用方照常展示。
//
// getter 由调用方提供：cmd 与 tui 各有已认证的 env/text 管理器，本包不重复
// 构造，避免在子命令里二次解锁 vault。
func ExportVars(envMgr *env.Manager, getter ref.ValueGetter) (map[string]string, []string, error) {
	vars, err := envMgr.ExportVariables()
	if err != nil {
		return nil, nil, err
	}
	if len(vars) == 0 {
		return nil, nil, nil
	}

	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	opts := ref.ResolveOptions{Loose: true}
	resolved := make(map[string]string, len(vars))
	var warnings []string
	for _, key := range keys {
		value, keyWarnings, err := ref.ResolveWithWarnings(vars[key], getter, opts)
		if err != nil {
			return nil, warnings, fmt.Errorf("resolve environment variable %s: %w", key, err)
		}
		for _, w := range keyWarnings {
			warnings = append(warnings, fmt.Sprintf("%s: %s", key, w))
		}
		resolved[key] = value
	}

	collisions, err := envMgr.KeyCollisionWarnings()
	if err != nil {
		return nil, nil, err
	}
	warnings = append(collisions, warnings...)
	return resolved, warnings, nil
}
