package tui

import (
	"fmt"

	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/ref"
	"github.com/wii/senv/internal/text"
)

// tuiGetter adapts the TUI's env+text managers into a ref.ValueGetter.
//
// This mirrors cmd.combinedGetter (in cmd/text.go) but lives in the tui package
// to avoid an import cycle: cmd depends on tui (to launch the program), so tui
// cannot import cmd. Centralizing it here lets every tab resolve references
// against the already-authenticated managers without re-prompting.
type tuiGetter struct {
	envMgr  *env.Manager
	textMgr *text.Manager
}

func (g tuiGetter) GetEnvValue(group, key string) (string, error) {
	return g.envMgr.Get(group, key)
}

func (g tuiGetter) GetTextValue(group, key string) (string, error) {
	return g.textMgr.Get(group, key)
}

// derefResult is the resolution outcome for a single value.
type derefResult struct {
	resolved string   // resolved value (or the original on failure)
	warnings []string // ref resolution warnings, if any
	failed   bool     // true if resolution errored
}

// resolveValues dereferences a set of raw values using the managers. On error
// for a given value the original value is kept and the failure is flagged so
// the UI can report it without crashing.
func resolveValues(mgr Managers, currentGroup string, items []envItemRow) (map[string]derefResult, error) {
	if mgr.Env == nil || mgr.Text == nil {
		return nil, fmt.Errorf("managers unavailable for dereference")
	}
	getter := tuiGetter{envMgr: mgr.Env, textMgr: mgr.Text}
	opts := ref.ResolveOptions{CurrentGroup: currentGroup}
	out := make(map[string]derefResult, len(items))
	for _, it := range items {
		r, warnings, err := ref.ResolveWithWarnings(it.value, getter, opts)
		if err != nil {
			out[it.key] = derefResult{resolved: it.value, failed: true}
			continue
		}
		out[it.key] = derefResult{resolved: r, warnings: warnings}
	}
	return out, nil
}
