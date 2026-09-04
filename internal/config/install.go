package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"time"

	"github.com/wii/senv/internal/exportfile"
	"github.com/wii/senv/internal/storage"
)

var (
	readPlainFile   = exportfile.ReadFile
	writePlainFile  = exportfile.WriteFile
	removePlainFile = exportfile.RemoveFile
	nowForBackup    = time.Now
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
	if scope.Name != "" {
		if err := validateConfigName(scope.Name); err != nil {
			return nil, nil, err
		}
	}
	if scope.Group != "" {
		group, err := normalizeConfigGroup(scope.Group)
		if err != nil {
			return nil, nil, err
		}
		scope.Group = group
	}
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
	existing, _, err := readPlainFile(targetPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
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
// returns the backup path. Its mode is no wider than either the source or the
// requested target contract.
func backupFile(targetPath string, requestedMode fs.FileMode) (string, error) {
	data, sourceMode, err := readPlainFile(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to read %s for backup: %w", targetPath, err)
	}
	return backupFileData(targetPath, data, sourceMode, requestedMode)
}

func backupFileData(targetPath string, data []byte, sourceMode, requestedMode fs.FileMode) (string, error) {
	backupPath := fmt.Sprintf("%s.senv-backup-%s", targetPath, nowForBackup().Format("20060102150405"))
	backupMode := sourceMode.Perm() & requestedMode.Perm()
	if err := writePlainFile(backupPath, data, backupMode); err != nil {
		return "", fmt.Errorf("failed to write backup %s: %w", backupPath, err)
	}
	return backupPath, nil
}

// installOne writes decrypted content atomically to targetPath, creating only
// missing parents privately and backing up an existing differing file first.
// Returns the backup path ("" when no backup was needed).
func installOne(content []byte, targetPath string, mode fs.FileMode) (backupPath string, err error) {
	if mode&^fs.ModePerm != 0 {
		return "", fmt.Errorf("invalid file mode %04o: special bits are not supported", mode)
	}
	existing, existingMode, readErr := readPlainFile(targetPath)
	switch {
	case errors.Is(readErr, os.ErrNotExist):
		// New target: no backup.
	case readErr != nil:
		return "", fmt.Errorf("failed to read target %s: %w", targetPath, readErr)
	case bytes.Equal(existing, content):
		// Still rewrite atomically so a loose existing mode is tightened.
		if err := writePlainFile(targetPath, content, mode); err != nil {
			return "", fmt.Errorf("failed to write target file: %w", err)
		}
		return "", nil
	default:
		backupPath, err = backupFileData(targetPath, existing, existingMode, mode)
		if err != nil {
			return "", err
		}
	}

	if err := writePlainFile(targetPath, content, mode); err != nil {
		return backupPath, fmt.Errorf("failed to write target file: %w", err)
	}
	return backupPath, nil
}

// ExecuteInstall runs a previously confirmed plan with the private default
// mode.
func (m *Manager) ExecuteInstall(plan *InstallPlan) error {
	return m.ExecuteInstallWithMode(plan, exportfile.DefaultMode)
}

// ExecuteInstallWithMode runs a confirmed plan using a mode selected for this
// operation only.
func (m *Manager) ExecuteInstallWithMode(plan *InstallPlan, mode fs.FileMode) error {
	if plan == nil {
		return fmt.Errorf("install plan is nil")
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
		backupPath, err := installOne(content, item.TargetPath, mode)
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
