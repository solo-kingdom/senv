package storage

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/wii/senv/internal/crypto"
)

// MigrateEnvGroupIfNeeded checks for an old-format env_<group>.json.enc file
// and converts it to the new per-variable layout. Returns true if migration ran.
func (m *Manager) MigrateEnvGroupIfNeeded(group string, key []byte) (bool, error) {
	if err := ValidateName(group); err != nil {
		return false, fmt.Errorf("invalid env group %q: %w", group, err)
	}
	if !m.mutationLocked {
		var migrated bool
		err := m.mutate(func(locked *Manager) error {
			var err error
			migrated, err = locked.MigrateEnvGroupIfNeeded(group, key)
			return err
		})
		return migrated, err
	}
	if err := m.requireCurrentKey(key); err != nil {
		return false, err
	}
	root, err := m.openDataRoot()
	if err != nil {
		return false, err
	}
	legacyName := fmt.Sprintf("%s%s%s", EnvFilePrefix, group, EnvFileSuffix)
	data, err := root.Read(legacyName)
	root.Close()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	decrypted, err := crypto.Decrypt(key, string(data))
	if err != nil {
		return false, fmt.Errorf("decrypt old env group %q: %w", group, err)
	}
	var envGroup EnvGroup
	if err := FromJSON(decrypted, &envGroup); err != nil {
		return false, fmt.Errorf("parse old env group %q: %w", group, err)
	}
	if envGroup.Name != group {
		return false, fmt.Errorf("legacy env group identity mismatch: file %q, Name %q", group, envGroup.Name)
	}
	if err := validateEnvGroup(&envGroup); err != nil {
		return false, fmt.Errorf("invalid legacy env group %q: %w", group, err)
	}

	meta := &EnvGroupMeta{Name: group, CreatedAt: envGroup.CreatedAt}
	if err := m.SaveEnvGroupMetaWithKey(group, meta, key); err != nil {
		return false, err
	}
	for variable, value := range envGroup.Variables {
		entry := &EnvVarEntry{Value: value, CreatedAt: envGroup.CreatedAt, UpdatedAt: envGroup.UpdatedAt}
		if err := m.SaveEnvVarWithKey(group, variable, entry, key); err != nil {
			return false, err
		}
	}
	root, err = m.openDataRoot()
	if err != nil {
		return false, err
	}
	defer root.Close()
	if err := removeManagedFile(root, legacyName); err != nil {
		return false, fmt.Errorf("remove old file after migration: %w", err)
	}
	return true, nil
}

// MigrateAllEnvGroups migrates all old-format env groups to per-variable storage.
func (m *Manager) MigrateAllEnvGroups(key []byte) (int, error) {
	if !m.mutationLocked {
		var count int
		err := m.mutate(func(locked *Manager) error {
			var err error
			count, err = locked.MigrateAllEnvGroups(key)
			return err
		})
		return count, err
	}
	root, err := m.openDataRoot()
	if err != nil {
		return 0, err
	}
	entries, err := root.ReadDir()
	root.Close()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir || !strings.HasPrefix(entry.Name, EnvFilePrefix) || !strings.HasSuffix(entry.Name, EnvFileSuffix) {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(entry.Name, EnvFilePrefix), EnvFileSuffix)
		if err := ValidateName(name); err != nil {
			return count, fmt.Errorf("invalid historical env group %q: %w", name, err)
		}
		migrated, err := m.MigrateEnvGroupIfNeeded(name, key)
		if err != nil {
			return count, fmt.Errorf("migrate group %q: %w", name, err)
		}
		if migrated {
			count++
		}
	}
	return count, nil
}
