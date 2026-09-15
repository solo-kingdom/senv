package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/ref"
)

// resolveExportShell resolves each active-group env variable with loose
// semantics (missing targets keep {{...}} and emit warnings). Structural
// errors such as cycles still fail the whole export. currentGroup applies to
// references without an explicit group (same as -g/--group).
func resolveExportShell(envMgr *env.Manager, textMgr textValueGetter, currentGroup string) (shell string, warnings []string, err error) {
	vars, err := envMgr.ExportVariables()
	if err != nil {
		return "", nil, err
	}
	if len(vars) == 0 {
		return "", nil, nil
	}

	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	getter := newRefGetter(envMgr, textMgr)
	opts := ref.ResolveOptions{Loose: true, CurrentGroup: currentGroup}

	resolved := make(map[string]string, len(vars))
	for _, key := range keys {
		value, keyWarnings, resolveErr := ref.ResolveWithWarnings(vars[key], getter, opts)
		if resolveErr != nil {
			return "", warnings, resolveErr
		}
		for _, w := range keyWarnings {
			warnings = append(warnings, formatExportWarning(key, w))
		}
		resolved[key] = value
	}
	return env.FormatExportShell(resolved), warnings, nil
}

func formatExportWarning(envKey, refWarning string) string {
	msg := strings.TrimSpace(refWarning)
	msg = strings.TrimPrefix(msg, "warning:")
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return fmt.Sprintf("warning: env export: %s", envKey)
	}
	return fmt.Sprintf("warning: env export: %s: %s", envKey, msg)
}
