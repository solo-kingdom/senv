package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wii/senv/internal/crypto"
)

// MigrateEnvGroupIfNeeded checks for an old-format env_<group>.json.enc file
// and converts it to the new per-variable layout. Returns true if migration ran.
func (m *Manager) MigrateEnvGroupIfNeeded(group string, key []byte) (bool, error) {
	oldPath := filepath.Join(m.dataPath, fmt.Sprintf("%s%s%s", EnvFilePrefix, group, EnvFileSuffix))
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return false, nil
	}

	data, err := os.ReadFile(oldPath)
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

	groupDir := m.envGroupDir(group)
	if err := os.MkdirAll(groupDir, 0o700); err != nil {
		return false, err
	}

	meta := &EnvGroupMeta{Name: envGroup.Name, CreatedAt: envGroup.CreatedAt}
	if err := m.SaveEnvGroupMetaWithKey(group, meta, key); err != nil {
		return false, err
	}

	for k, v := range envGroup.Variables {
		entry := &EnvVarEntry{Value: v, CreatedAt: envGroup.CreatedAt, UpdatedAt: envGroup.UpdatedAt}
		if err := m.SaveEnvVarWithKey(group, k, entry, key); err != nil {
			return false, err
		}
	}

	if err := os.Remove(oldPath); err != nil {
		return false, fmt.Errorf("remove old file after migration: %w", err)
	}
	return true, nil
}

// MigrateAllEnvGroups migrates all old-format env groups to per-variable storage.
// Returns the number of groups migrated.
func (m *Manager) MigrateAllEnvGroups(key []byte) (int, error) {
	files, err := os.ReadDir(m.dataPath)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, file := range files {
		if !strings.HasPrefix(file.Name(), EnvFilePrefix) || !strings.HasSuffix(file.Name(), EnvFileSuffix) {
			continue
		}
		name := strings.TrimPrefix(file.Name(), EnvFilePrefix)
		name = strings.TrimSuffix(name, EnvFileSuffix)
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
