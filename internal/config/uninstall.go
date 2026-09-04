package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
)

// UninstallItem is a single planned uninstall action.
type UninstallItem struct {
	Name       string
	Group      string
	TargetPath string
	Action     string // noop | delete | changed | error
	Reason     string
	Err        error
}

// UninstallPlan is the dry-run result of an uninstall operation.
type UninstallPlan struct {
	Items []UninstallItem
}

// HasChanged reports whether the plan contains items whose target file was
// modified locally and therefore need explicit per-item confirmation.
func (p *UninstallPlan) HasChanged() bool {
	for _, item := range p.Items {
		if item.Action == ActionChanged {
			return true
		}
	}
	return false
}

// uninstallActionFor compares the decrypted content with the current target
// file and returns the uninstall action for it.
func uninstallActionFor(content []byte, targetPath string) (action string, reason string) {
	existing, _, err := readPlainFile(targetPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return ActionNoop, "target already absent"
	case err != nil:
		return ActionError, err.Error()
	case bytes.Equal(existing, content):
		return ActionDelete, "target matches stored content"
	default:
		return ActionChanged, "target was modified locally; requires confirmation"
	}
}

// PlanUninstall computes the uninstall plan for a scope without touching the
// filesystem.
func (m *Manager) PlanUninstall(scope Scope) (*UninstallPlan, error) {
	names, configIndex, err := m.resolveScope(scope)
	if err != nil {
		return nil, err
	}

	plan := &UninstallPlan{}
	for _, name := range names {
		cfg := configIndex.Configs[name]
		item := UninstallItem{Name: name, Group: cfg.NormalizedGroup()}

		targetPath, err := ResolveTargetPath(cfg.TargetPath)
		if err != nil {
			item.Action = ActionError
			item.Err = err
			item.Reason = err.Error()
			plan.Items = append(plan.Items, item)
			continue
		}
		item.TargetPath = targetPath

		content, err := m.loadConfigFile(name)
		if err != nil {
			item.Action = ActionError
			item.Err = err
			item.Reason = err.Error()
			plan.Items = append(plan.Items, item)
			continue
		}

		item.Action, item.Reason = uninstallActionFor(content, targetPath)
		plan.Items = append(plan.Items, item)
	}
	return plan, nil
}

// ExecuteUninstall runs a confirmed uninstall plan. Only the target files are
// removed — never the encrypted storage entries and never directories.
// confirmChanged is consulted for each item whose target was modified; a nil
// confirmChanged rejects them all. The target is re-compared right before
// removal so a file changed since planning is not silently deleted.
func (m *Manager) ExecuteUninstall(plan *UninstallPlan, confirmChanged func(item UninstallItem) bool) error {
	if plan == nil {
		return fmt.Errorf("uninstall plan is nil")
	}
	for _, item := range plan.Items {
		if err := validateConfigName(item.Name); err != nil {
			return err
		}
		if _, err := normalizeConfigGroup(item.Group); err != nil {
			return err
		}
	}
	var errs []string
	for _, item := range plan.Items {
		switch item.Action {
		case ActionNoop:
			fmt.Printf("- %s: nothing to do (%s)\n", item.Name, item.Reason)
			continue
		case ActionError:
			fmt.Printf("- %s: skipped (%s)\n", item.Name, item.Reason)
			errs = append(errs, fmt.Sprintf("%s: %s", item.Name, item.Reason))
			continue
		}

		if item.Action == ActionChanged {
			if confirmChanged == nil || !confirmChanged(item) {
				fmt.Printf("- %s: kept (%s)\n", item.Name, item.TargetPath)
				continue
			}
		}

		// Re-check: do not delete a file that no longer matches expectations.
		content, err := m.loadConfigFile(item.Name)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", item.Name, err))
			continue
		}
		current, _ := uninstallActionFor(content, item.TargetPath)
		if item.Action == ActionDelete && current != ActionDelete {
			fmt.Printf("- %s: skipped (state changed since plan)\n", item.Name)
			continue
		}

		if err := removePlainFile(item.TargetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Sprintf("%s: %v", item.Name, err))
			continue
		}
		fmt.Printf("- %s: removed %s\n", item.Name, item.TargetPath)
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d item(s) failed: %v", len(errs), errs)
	}
	return nil
}
