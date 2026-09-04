package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigRepairItem describes one planned fix for a quarantined legacy config
// entry: a deterministic portable replacement name plus a file-existence
// preflight result.
type ConfigRepairItem struct {
	// OldName is the current, non-portable index key.
	OldName string
	// NewName is the suggested portable replacement.
	NewName string
	// Record is the normalized index entry (canonical EncryptedFile, full
	// metadata) so repair preserves description, target path and timestamps.
	Record ConfigFile
	// MissingFile reports that the ciphertext for this entry no longer exists.
	// Such stale entries can only be dropped, and only with explicit consent.
	MissingFile bool
}

// SuggestPortableConfigName applies the deterministic legacy rewrite rule:
// every non-portable ":" becomes "_". The result must still pass single-
// segment validation; callers treat a validation failure as a hard planning
// error rather than guessing a different name.
func SuggestPortableConfigName(name string) string {
	return strings.ReplaceAll(name, ":", "_")
}

// PlanConfigRepair builds the full repair plan for quarantined index entries.
// It fails before any filesystem change when a suggested name is invalid or
// collides with an existing entry (or another suggestion). Entries whose
// ciphertext is missing are marked MissingFile; the caller decides whether to
// allow dropping them.
func (m *Manager) PlanConfigRepair() ([]ConfigRepairItem, error) {
	index, quarantined, err := m.LoadConfigIndexWithQuarantine()
	if err != nil {
		return nil, err
	}
	taken := make(map[string]bool, len(index.Configs)+len(quarantined))
	for name := range index.Configs {
		taken[name] = true
	}
	items := make([]ConfigRepairItem, 0, len(quarantined))
	for _, q := range quarantined {
		suggestion := SuggestPortableConfigName(q.Name)
		if err := ValidateName(suggestion); err != nil {
			return nil, fmt.Errorf("cannot suggest portable name for %q: %w", q.Name, err)
		}
		if taken[suggestion] {
			return nil, fmt.Errorf("suggested repair name %q for %q conflicts with an existing config", suggestion, q.Name)
		}
		taken[suggestion] = true
		encrypted := q.Record.EncryptedFile
		if encrypted == "" {
			encrypted = q.Name + ConfigFileSuffix
		}
		exists, err := m.legacyConfigFileExists(encrypted)
		if err != nil {
			return nil, err
		}
		items = append(items, ConfigRepairItem{
			OldName:     q.Name,
			NewName:     suggestion,
			Record:      q.Record,
			MissingFile: !exists,
		})
	}
	return items, nil
}

// ExecuteConfigRepair applies an explicit repair decision set under the vault
// mutation lock: every quarantined entry must be either renamed or (when
// missing and dropMissing is set) dropped. File renames happen before the
// index rewrite; a mid-flight failure attempts to roll back already-renamed
// files so the on-disk index stays authoritative.
func (m *Manager) ExecuteConfigRepair(renames map[string]string, dropMissing bool) error {
	return m.mutate(func(locked *Manager) error {
		return locked.executeConfigRepairLocked(renames, dropMissing)
	})
}

func (m *Manager) executeConfigRepairLocked(renames map[string]string, dropMissing bool) error {
	index, quarantined, err := m.LoadConfigIndexWithQuarantine()
	if err != nil {
		return err
	}
	if len(quarantined) == 0 {
		return fmt.Errorf("no quarantined configs to repair")
	}

	byOld := make(map[string]ConfigQuarantine, len(quarantined))
	for _, q := range quarantined {
		byOld[q.Name] = q
	}
	repaired := &ConfigIndex{Configs: make(map[string]ConfigFile, len(index.Configs)+len(quarantined))}
	for name, config := range index.Configs {
		repaired.Configs[name] = config
	}

	type fileRename struct{ from, to string }
	var renamesToDo []fileRename
	seenNew := make(map[string]bool, len(renames))
	for _, q := range quarantined {
		newName, shouldRename := renames[q.Name]
		if !shouldRename {
			exists, err := m.legacyConfigFileExists(q.Record.EncryptedFile)
			if err != nil {
				return err
			}
			if !exists {
				if !dropMissing {
					return fmt.Errorf("ciphertext for %q is missing; rerun with --drop-missing to drop the stale entry", q.Name)
				}
				delete(repaired.Configs, q.Name)
				continue
			}
			return fmt.Errorf("quarantined config %q has no repair decision; rename it or rerun with --drop-missing when its file is missing", q.Name)
		}
		if err := ValidateName(newName); err != nil {
			return fmt.Errorf("invalid repair name for %q: %w", q.Name, err)
		}
		if _, exists := index.Configs[newName]; exists || seenNew[newName] {
			return fmt.Errorf("repair name %q for %q conflicts with an existing config", newName, q.Name)
		}
		seenNew[newName] = true

		// A rename decision still requires the ciphertext to exist; stale
		// entries must be dropped explicitly via dropMissing instead.
		exists, err := m.legacyConfigFileExists(q.Record.EncryptedFile)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("ciphertext for %q is missing; rerun with --drop-missing to drop the stale entry", q.Name)
		}

		record := q.Record
		record.Name = newName
		record.EncryptedFile = newName + ConfigFileSuffix
		if isNonPortableOnly(record.Group) {
			record.Group = SuggestPortableConfigName(record.Group)
			if err := ValidateName(record.Group); err != nil {
				return fmt.Errorf("cannot sanitize group for %q: %w", q.Name, err)
			}
		}

		oldPath, err := m.legacyConfigFilePath(q.Record.EncryptedFile)
		if err != nil {
			return err
		}
		newPath, err := m.legacyConfigFilePath(record.EncryptedFile)
		if err != nil {
			return err
		}
		if err := checkRepairRenameTarget(oldPath, newPath); err != nil {
			return err
		}
		renamesToDo = append(renamesToDo, fileRename{from: oldPath, to: newPath})
		repaired.Configs[newName] = record
		delete(repaired.Configs, q.Name)
	}

	// All decisions validated: rename files first, then rewrite the index. On
	// failure, roll back completed renames so the old index stays truthful.
	completed := make([]fileRename, 0, len(renamesToDo))
	rollback := func(cause error) error {
		for i := len(completed) - 1; i >= 0; i-- {
			_ = os.Rename(completed[i].to, completed[i].from)
		}
		return cause
	}
	for _, rn := range renamesToDo {
		if err := os.Rename(rn.from, rn.to); err != nil {
			return rollback(fmt.Errorf("rename config ciphertext %q -> %q: %w", filepath.Base(rn.from), filepath.Base(rn.to), err))
		}
		completed = append(completed, rn)
	}
	if err := m.SaveConfigIndex(repaired); err != nil {
		return rollback(fmt.Errorf("save repaired config index: %w", err))
	}
	return nil
}

// legacyConfigFilePath resolves a legacy (possibly ":") ciphertext filename
// inside the data root after a lexical fence check. It exists only for repair:
// every regular code path must keep addressing files through securefs.
func (m *Manager) legacyConfigFilePath(encryptedName string) (string, error) {
	if hasStructuralIdentityHazard(encryptedName) {
		return "", fmt.Errorf("legacy ciphertext name %q has a structural identity hazard", encryptedName)
	}
	root, err := filepath.Abs(m.dataPath)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, encryptedName)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", fmt.Errorf("legacy ciphertext path escapes data root")
	}
	return path, nil
}

func (m *Manager) legacyConfigFileExists(encryptedName string) (bool, error) {
	path, err := m.legacyConfigFilePath(encryptedName)
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("legacy ciphertext %q is not a regular file", encryptedName)
	}
	return true, nil
}

// checkRepairRenameTarget enforces that the source exists as a regular file,
// the destination is absent, and neither endpoint is a symbolic link.
func checkRepairRenameTarget(oldPath, newPath string) error {
	oldInfo, err := os.Lstat(oldPath)
	if err != nil {
		return fmt.Errorf("inspect legacy ciphertext %q: %w", filepath.Base(oldPath), err)
	}
	if !oldInfo.Mode().IsRegular() {
		return fmt.Errorf("legacy ciphertext %q is not a regular file", filepath.Base(oldPath))
	}
	if newInfo, err := os.Lstat(newPath); err == nil {
		if newInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("repair target %q is a symbolic link", filepath.Base(newPath))
		}
		return fmt.Errorf("repair target %q already exists", filepath.Base(newPath))
	} else if !os.IsNotExist(err) {
		return err
	}
	if oldInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("legacy ciphertext %q is a symbolic link", filepath.Base(oldPath))
	}
	return nil
}
