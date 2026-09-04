package storage

import (
	"errors"
	"fmt"
	"os"

	"github.com/wii/senv/internal/securefs"
)

type trustedRootOpener func(string) (securefs.TrustedRoot, error)

func defaultTrustedRootOpener(path string) (securefs.TrustedRoot, error) {
	return securefs.OpenRoot(path)
}

func (m *Manager) openConfigRoot() (securefs.TrustedRoot, error) {
	if err := EnsurePrivateDir(m.configPath, 0o700); err != nil {
		return nil, fmt.Errorf("prepare config root: %w", err)
	}
	root, err := m.openRoot(m.configPath)
	if err != nil {
		return nil, fmt.Errorf("open config root: %w", err)
	}
	return root, nil
}

func (m *Manager) openDataRoot() (securefs.TrustedRoot, error) {
	if err := EnsurePrivateDir(m.dataPath, 0o700); err != nil {
		return nil, fmt.Errorf("prepare data root: %w", err)
	}
	root, err := m.openRoot(m.dataPath)
	if err != nil {
		return nil, fmt.Errorf("open data root: %w", err)
	}
	return root, nil
}

func removeManagedFile(root securefs.TrustedRoot, segments ...string) error {
	if err := root.Remove(segments...); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ValidateName applies the portable single-path-segment storage identity rule.
func ValidateName(name string) error {
	return securefs.ValidateSegment(name)
}

func validateEnvIdentity(group, key string) error {
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid env group %q: %w", group, err)
	}
	if err := ValidateName(key); err != nil {
		return fmt.Errorf("invalid env key %q: %w", key, err)
	}
	if err := ValidateEnvKey(key); err != nil {
		return err
	}
	return nil
}

func validateTextIdentity(group, key string) error {
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid text group %q: %w", group, err)
	}
	if err := ValidateName(key); err != nil {
		return fmt.Errorf("invalid text key %q: %w", key, err)
	}
	return nil
}

func validateConfigName(name string) error {
	if err := ValidateName(name); err != nil {
		return fmt.Errorf("invalid config name %q: %w", name, err)
	}
	return nil
}

func validateConfigGroup(group string) error {
	if group == "" {
		group = ConfigDefaultGroup
	}
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid config group %q: %w", group, err)
	}
	return nil
}

func validateSettings(settings *Settings) error {
	if settings == nil {
		return fmt.Errorf("settings are nil")
	}
	if err := ValidateName(settings.DefaultGroup); err != nil {
		return fmt.Errorf("invalid default env group %q: %w", settings.DefaultGroup, err)
	}
	for _, group := range settings.ActiveGroups {
		if err := ValidateName(group); err != nil {
			return fmt.Errorf("invalid active env group %q: %w", group, err)
		}
	}
	return nil
}

func validateEnvGroup(envGroup *EnvGroup) error {
	if envGroup == nil {
		return fmt.Errorf("env group is nil")
	}
	if err := ValidateName(envGroup.Name); err != nil {
		return fmt.Errorf("invalid env group %q: %w", envGroup.Name, err)
	}
	for key := range envGroup.Variables {
		if err := validateEnvIdentity(envGroup.Name, key); err != nil {
			return err
		}
	}
	return nil
}

// normalizeConfigIndex validates every identity before any indexed file can be
// read or deleted. Legacy empty EncryptedFile values are normalized only in the
// returned in-memory object.
func normalizeConfigIndex(index *ConfigIndex) (*ConfigIndex, error) {
	if index == nil || index.Configs == nil {
		return nil, fmt.Errorf("config index has nil configs map")
	}
	normalized := &ConfigIndex{Configs: make(map[string]ConfigFile, len(index.Configs))}
	for mapName, config := range index.Configs {
		if err := validateConfigName(mapName); err != nil {
			return nil, fmt.Errorf("invalid config index map key: %w", err)
		}
		if err := validateConfigName(config.Name); err != nil {
			return nil, fmt.Errorf("invalid config index Name for %q: %w", mapName, err)
		}
		if config.Name != mapName {
			return nil, fmt.Errorf("config index identity mismatch: map key %q does not match Name %q", mapName, config.Name)
		}
		if err := validateConfigGroup(config.Group); err != nil {
			return nil, fmt.Errorf("invalid config index Group for %q: %w", mapName, err)
		}
		canonical := mapName + ConfigFileSuffix
		if config.EncryptedFile == "" {
			config.EncryptedFile = canonical
		} else {
			if err := ValidateName(config.EncryptedFile); err != nil {
				return nil, fmt.Errorf("invalid config index EncryptedFile for %q: %w", mapName, err)
			}
			if config.EncryptedFile != canonical {
				return nil, fmt.Errorf("config index encrypted file mismatch for %q: got %q, want %q", mapName, config.EncryptedFile, canonical)
			}
		}
		normalized.Configs[mapName] = config
	}
	return normalized, nil
}
