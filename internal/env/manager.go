package env

import (
	"encoding/base64"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/storage"
)

// Manager handles environment variable operations
type Manager struct {
	storage        *storage.Manager
	password       string
	key            []byte
	mutationLocked bool
}

// NewManager creates a new environment variable manager
func NewManager(storage *storage.Manager, password string) *Manager {
	return &Manager{
		storage:  storage,
		password: password,
	}
}

// NewManagerWithKey creates a new environment variable manager with a derived key
func NewManagerWithKey(storage *storage.Manager, key []byte) *Manager {
	return &Manager{
		storage: storage,
		key:     key,
	}
}

func (m *Manager) mutate(fn func(*Manager) error) error {
	if m.mutationLocked {
		return fn(m)
	}
	return m.storage.WithVaultMutation(func(locked *storage.Manager) error {
		clone := *m
		clone.storage = locked
		clone.mutationLocked = true
		return fn(&clone)
	})
}

func validateGroup(group string) error {
	if err := storage.ValidateName(group); err != nil {
		return fmt.Errorf("invalid env group %q: %w", group, err)
	}
	return nil
}

func validateIdentity(group, key string) error {
	if err := validateGroup(group); err != nil {
		return err
	}
	if err := storage.ValidateName(key); err != nil {
		return fmt.Errorf("invalid env key %q: %w", key, err)
	}
	if err := storage.ValidateEnvKey(key); err != nil {
		return fmt.Errorf("invalid env key %q: %w", key, err)
	}
	return nil
}

// loadEnvGroup loads an environment variable group using key or password
func (m *Manager) loadEnvGroup(group string) (*storage.EnvGroup, error) {
	if m.key != nil {
		return m.storage.LoadEnvGroupWithKey(group, m.key)
	}
	return m.storage.LoadEnvGroup(group, m.password)
}

// saveEnvGroup saves an environment variable group using key or password
func (m *Manager) saveEnvGroup(envGroup *storage.EnvGroup) error {
	if m.key != nil {
		return m.storage.SaveEnvGroupWithKey(envGroup, m.key)
	}
	return m.storage.SaveEnvGroup(envGroup, m.password)
}

func (m *Manager) resolveCryptoKey() ([]byte, error) {
	if m.key != nil {
		return m.key, nil
	}
	md, err := m.storage.LoadMetadata()
	if err != nil {
		return nil, err
	}
	salt, err := base64.StdEncoding.DecodeString(md.Salt)
	if err != nil {
		return nil, err
	}
	iterations, err := md.ValidatedKDFIterations()
	if err != nil {
		return nil, err
	}
	return crypto.DeriveKeyWithIterations(m.password, salt, iterations), nil
}

// Get retrieves an environment variable from a group
func (m *Manager) Get(group string, key string) (string, error) {
	if err := validateIdentity(group, key); err != nil {
		return "", err
	}
	cryptoKey, err := m.resolveCryptoKey()
	if err != nil {
		return "", err
	}

	entry, err := m.storage.LoadEnvVarWithKey(group, key, cryptoKey)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("variable %s not found in group %s", key, group)
		}
		// Fall back to group load (handles old-format groups not yet migrated)
		envGroup, loadErr := m.loadEnvGroup(group)
		if loadErr != nil {
			return "", fmt.Errorf("failed to load group %s: %w", group, loadErr)
		}
		value, exists := envGroup.Variables[key]
		if !exists {
			return "", fmt.Errorf("variable %s not found in group %s", key, group)
		}
		return value, nil
	}
	return entry.Value, nil
}

// Set sets an environment variable in a group
func (m *Manager) Set(group string, key string, value string) error {
	if err := validateIdentity(group, key); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.Set(group, key, value) })
	}

	cryptoKey, err := m.resolveCryptoKey()
	if err != nil {
		return err
	}

	// Ensure group exists (migrate old format if needed)
	exists, err := m.storage.EnvGroupExists(group)
	if err != nil {
		return err
	}
	if !exists {
		envGroup := storage.NewEnvGroup(group)
		if err := m.saveEnvGroup(envGroup); err != nil {
			return fmt.Errorf("failed to create group %s: %w", group, err)
		}
	} else if _, err := m.storage.LoadEnvGroupMetaWithKey(group, cryptoKey); err != nil {
		// Old format exists but not yet migrated — trigger migration
		if _, err := m.loadEnvGroup(group); err != nil {
			return fmt.Errorf("failed to load group %s: %w", group, err)
		}
	}

	now := time.Now()
	entry := &storage.EnvVarEntry{Value: value, CreatedAt: now, UpdatedAt: now}

	// Preserve CreatedAt if the variable already exists
	if existing, err := m.storage.LoadEnvVarWithKey(group, key, cryptoKey); err == nil {
		entry.CreatedAt = existing.CreatedAt
	}

	if err := m.storage.SaveEnvVarWithKey(group, key, entry, cryptoKey); err != nil {
		return fmt.Errorf("failed to save variable %s: %w", key, err)
	}
	return nil
}

// Delete deletes an environment variable from a group
func (m *Manager) Delete(group string, key string) error {
	if err := validateIdentity(group, key); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.Delete(group, key) })
	}
	cryptoKey, err := m.resolveCryptoKey()
	if err != nil {
		return err
	}

	// Check existence (triggers migration if old format)
	if _, err := m.storage.LoadEnvVarWithKey(group, key, cryptoKey); err != nil {
		if os.IsNotExist(err) {
			// Maybe old format not yet migrated
			envGroup, loadErr := m.loadEnvGroup(group)
			if loadErr != nil {
				return fmt.Errorf("failed to load group %s: %w", group, loadErr)
			}
			if _, exists := envGroup.Variables[key]; !exists {
				return fmt.Errorf("variable %s not found in group %s", key, group)
			}
			// Migration happened via loadEnvGroup, try delete again
			return m.storage.DeleteEnvVar(group, key)
		}
		return fmt.Errorf("failed to check variable %s: %w", key, err)
	}

	return m.storage.DeleteEnvVar(group, key)
}

// List lists all environment variables in a group (or all groups if group is empty)
func (m *Manager) List(group string) (map[string]map[string]string, error) {
	if group != "" {
		if err := validateGroup(group); err != nil {
			return nil, err
		}
	}
	result := make(map[string]map[string]string)

	if group != "" {
		envGroup, err := m.loadEnvGroup(group)
		if err != nil {
			return nil, fmt.Errorf("failed to load group %s: %w", group, err)
		}
		result[group] = envGroup.Variables
	} else {
		groups, err := m.storage.ListEnvGroups()
		if err != nil {
			return nil, fmt.Errorf("failed to list groups: %w", err)
		}

		for _, g := range groups {
			envGroup, err := m.loadEnvGroup(g)
			if err != nil {
				return nil, fmt.Errorf("failed to load group %s: %w", g, err)
			}
			result[g] = envGroup.Variables
		}
	}

	return result, nil
}

// Export exports environment variables from active groups
func (m *Manager) Export() (string, error) {
	settings, err := m.storage.LoadSettings()
	if err != nil {
		return "", fmt.Errorf("failed to load settings: %w", err)
	}

	activeGroups := []string{settings.DefaultGroup}
	for _, g := range settings.ActiveGroups {
		if g != settings.DefaultGroup {
			activeGroups = append(activeGroups, g)
		}
	}

	allVars := make(map[string]string)
	for _, group := range activeGroups {
		if err := validateGroup(group); err != nil {
			return "", err
		}
		envGroup, err := m.loadEnvGroup(group)
		if err != nil {
			return "", fmt.Errorf("failed to load active group %s: %w", group, err)
		}

		for k, v := range envGroup.Variables {
			if err := validateIdentity(group, k); err != nil {
				return "", fmt.Errorf("invalid historical env identity: %w", err)
			}
			allVars[k] = v
		}
	}

	var lines []string
	keys := make([]string, 0, len(allVars))
	for k := range allVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := allVars[key]
		escapedValue := strings.ReplaceAll(value, "'", "'\\''")
		lines = append(lines, fmt.Sprintf("export %s='%s'", key, escapedValue))
	}

	return strings.Join(lines, "\n"), nil
}

// AddGroup creates a new environment variable group
func (m *Manager) AddGroup(name string) error {
	if err := validateGroup(name); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.AddGroup(name) })
	}
	groups, err := m.storage.ListEnvGroups()
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}

	for _, g := range groups {
		if g == name {
			return fmt.Errorf("group %s already exists", name)
		}
	}

	envGroup := storage.NewEnvGroup(name)
	if err := m.saveEnvGroup(envGroup); err != nil {
		return fmt.Errorf("failed to create group %s: %w", name, err)
	}

	return nil
}

// ActivateGroup activates a group by adding it to the active groups list
func (m *Manager) ActivateGroup(name string) error {
	if err := validateGroup(name); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.ActivateGroup(name) })
	}
	settings, err := m.storage.LoadSettings()
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	groups, err := m.storage.ListEnvGroups()
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}

	groupExists := false
	for _, g := range groups {
		if g == name {
			groupExists = true
			break
		}
	}

	if !groupExists {
		return fmt.Errorf("group %s does not exist", name)
	}

	if name == settings.DefaultGroup {
		return nil
	}

	for _, g := range settings.ActiveGroups {
		if g == name {
			return nil
		}
	}

	settings.ActiveGroups = append(settings.ActiveGroups, name)
	settings.UpdatedAt = time.Now().Format(time.RFC3339)

	if err := m.storage.SaveSettings(settings); err != nil {
		return fmt.Errorf("failed to save settings: %w", err)
	}

	return nil
}

// DeactivateGroup deactivates a group by removing it from the active groups list
func (m *Manager) DeactivateGroup(name string) error {
	if err := validateGroup(name); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.DeactivateGroup(name) })
	}
	settings, err := m.storage.LoadSettings()
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	if name == settings.DefaultGroup {
		return fmt.Errorf("cannot deactivate default group")
	}

	newActiveGroups := []string{}
	for _, g := range settings.ActiveGroups {
		if g != name {
			newActiveGroups = append(newActiveGroups, g)
		}
	}

	settings.ActiveGroups = newActiveGroups
	settings.UpdatedAt = time.Now().Format(time.RFC3339)

	if err := m.storage.SaveSettings(settings); err != nil {
		return fmt.Errorf("failed to save settings: %w", err)
	}

	return nil
}

// ListGroups lists all groups and their status
func (m *Manager) ListGroups() ([]GroupInfo, error) {
	groups, err := m.storage.ListEnvGroups()
	if err != nil {
		return nil, fmt.Errorf("failed to list groups: %w", err)
	}

	settings, err := m.storage.LoadSettings()
	if err != nil {
		return nil, fmt.Errorf("failed to load settings: %w", err)
	}

	var result []GroupInfo
	for _, name := range groups {
		isActive := name == settings.DefaultGroup
		if !isActive {
			for _, g := range settings.ActiveGroups {
				if g == name {
					isActive = true
					break
				}
			}
		}

		envGroup, err := m.loadEnvGroup(name)
		if err != nil {
			return nil, fmt.Errorf("failed to load group %s: %w", name, err)
		}
		varCount := len(envGroup.Variables)

		result = append(result, GroupInfo{
			Name:      name,
			IsActive:  isActive,
			VarCount:  varCount,
			IsDefault: name == settings.DefaultGroup,
		})
	}

	return result, nil
}

// GroupInfo represents information about a group
type GroupInfo struct {
	Name      string
	IsActive  bool
	VarCount  int
	IsDefault bool
}
