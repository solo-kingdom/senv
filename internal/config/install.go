package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/wii/senv/internal/storage"
)

// Scope selects which configs an install/uninstall operation applies to.
// Exactly one of Name / Group / All must be set.
type Scope struct {
	Name  string
	Group string
	All   bool
}

// Action identifiers used in plan items.
const (
	ActionCreate          = "create"           // target missing -> write new file
	ActionSkip            = "skip"             // target identical -> no write
	ActionBackupOverwrite = "backup_overwrite" // target differs -> backup then overwrite
	ActionNoop            = "noop"             // uninstall: target already missing
	ActionDelete          = "delete"           // uninstall: target identical -> remove
	ActionChanged         = "changed"          // uninstall: target modified -> needs explicit confirmation
	ActionError           = "error"            // item cannot be processed (see Err)
)

// InstallItem is a single planned install action.
type InstallItem struct {
	Name       string
	Group      string
	TargetPath string // resolved absolute/relative path after expansion
	Action     string
	Reason     string
	Err        error
}

// InstallPlan is the dry-run result of an install operation.
type InstallPlan struct {
	Items []InstallItem
}

// resolveScope validates the scope and returns the matching config names
// (sorted) together with the loaded index.
func (m *Manager) resolveScope(scope Scope) ([]string, *storage.ConfigIndex, error) {
	configIndex, err := m.storage.LoadConfigIndex()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config index: %w", err)
	}

	var names []string
	switch {
	case scope.Name != "" && (scope.Group != "" || scope.All):
		return nil, nil, fmt.Errorf("specify only one of: name, --group, --all")
	case scope.Name != "":
		if _, ok := configIndex.Configs[scope.Name]; !ok {
			return nil, nil, fmt.Errorf("config %s not found", scope.Name)
		}
		names = []string{scope.Name}
	case scope.Group != "":
		for name, cfg := range configIndex.Configs {
			if cfg.NormalizedGroup() == scope.Group {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			return nil, nil, fmt.Errorf("no configs in group %s", scope.Group)
		}
	case scope.All:
		for name := range configIndex.Configs {
			names = append(names, name)
		}
		if len(names) == 0 {
			return nil, nil, fmt.Errorf("no configs found")
		}
	default:
		return nil, nil, fmt.Errorf("specify a config name, --group, or --all")
	}
	sort.Strings(names)
	return names, configIndex, nil
}

// planItemState compares the decrypted content with the current target file
// and returns the install action for it.
func installActionFor(content []byte, targetPath string) (action string, reason string) {
	existing, err := os.ReadFile(targetPath)
	switch {
	case os.IsNotExist(err):
		return ActionCreate, "target does not exist"
	case err != nil:
		return ActionError, err.Error()
	case bytes.Equal(existing, content):
		return ActionSkip, "target is up to date"
	default:
		return ActionBackupOverwrite, "target content differs; will backup first"
	}
}

// PlanInstall computes the install plan for a scope without touching the
// filesystem (read-only: decrypt, resolve path, compare).
func (m *Manager) PlanInstall(scope Scope) (*InstallPlan, error) {
	names, configIndex, err := m.resolveScope(scope)
	if err != nil {
		return nil, err
	}

	plan := &InstallPlan{}
	for _, name := range names {
		cfg := configIndex.Configs[name]
		item := InstallItem{Name: name, Group: cfg.NormalizedGroup()}

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

		item.Action, item.Reason = installActionFor(content, targetPath)
		plan.Items = append(plan.Items, item)
	}
	return plan, nil
}

// backupFile copies the target to "<target>.senv-backup-<timestamp>" and
// returns the backup path.
func backupFile(targetPath string) (string, error) {
	data, err := os.ReadFile(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to read %s for backup: %w", targetPath, err)
	}
	info, err := os.Stat(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat %s: %w", targetPath, err)
	}
	backupPath := fmt.Sprintf("%s.senv-backup-%s", targetPath, time.Now().Format("20060102150405"))
	if err := os.WriteFile(backupPath, data, info.Mode().Perm()); err != nil {
		return "", fmt.Errorf("failed to write backup %s: %w", backupPath, err)
	}
	return backupPath, nil
}

// installOne writes decrypted content to targetPath, creating parent
// directories recursively and backing up an existing differing file first.
// Returns the backup path ("" when no backup was needed).
func installOne(content []byte, targetPath string) (backupPath string, err error) {
	action, _ := installActionFor(content, targetPath)
	if action == ActionSkip {
		return "", nil
	}
	if action == ActionError {
		return "", fmt.Errorf("failed to read target %s", targetPath)
	}

	if action == ActionBackupOverwrite {
		backupPath, err = backupFile(targetPath)
		if err != nil {
			return "", err
		}
	}

	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create target directory: %w", err)
	}
	if err := os.WriteFile(targetPath, content, 0o644); err != nil {
		return "", fmt.Errorf("failed to write target file: %w", err)
	}
	return backupPath, nil
}

// ExecuteInstall runs a previously confirmed plan. Items in error or skip
// state are not written. Each item re-checks the target right before writing
// so a file changed between plan and execute is still backed up.
func (m *Manager) ExecuteInstall(plan *InstallPlan) error {
	var errs []string
	for _, item := range plan.Items {
		switch item.Action {
		case ActionSkip:
			fmt.Printf("- %s: skipped (%s)\n", item.Name, item.Reason)
			continue
		case ActionError:
			fmt.Printf("- %s: skipped (%s)\n", item.Name, item.Reason)
			errs = append(errs, fmt.Sprintf("%s: %s", item.Name, item.Reason))
			continue
		}

		content, err := m.loadConfigFile(item.Name)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", item.Name, err))
			continue
		}
		backupPath, err := installOne(content, item.TargetPath)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", item.Name, err))
			continue
		}
		if backupPath != "" {
			fmt.Printf("- %s: installed to %s (backup: %s)\n", item.Name, item.TargetPath, backupPath)
		} else {
			fmt.Printf("- %s: installed to %s\n", item.Name, item.TargetPath)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d item(s) failed: %v", len(errs), errs)
	}
	return nil
}
