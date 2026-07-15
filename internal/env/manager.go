package env

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/wii/senv/internal/storage"
)

// Manager handles environment variable operations
type Manager struct {
	storage  *storage.Manager
	password string
	key      []byte
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

// Get retrieves an environment variable from a group
func (m *Manager) Get(group string, key string) (string, error) {
	envGroup, err := m.loadEnvGroup(group)
	if err != nil {
		return "", fmt.Errorf("failed to load group %s: %w", group, err)
	}

	value, exists := envGroup.Variables[key]
	if !exists {
		return "", fmt.Errorf("variable %s not found in group %s", key, group)
	}

	return value, nil
}

// Set sets an environment variable in a group
func (m *Manager) Set(group string, key string, value string) error {
	if err := storage.ValidateName(group); err != nil {
		return fmt.Errorf("invalid group: %w", err)
	}
	if err := storage.ValidateName(key); err != nil {
		return fmt.Errorf("invalid key: %w", err)
	}
	// Env keys are emitted as `export <key>=...` by env export, so they must
	// be valid POSIX shell variable names to avoid breaking `eval $(...)`.
	if err := storage.ValidateEnvKey(key); err != nil {
		return fmt.Errorf("invalid env key: %w", err)
	}

	envGroup, err := m.loadEnvGroup(group)
	if err != nil {
		envGroup = storage.NewEnvGroup(group)
	}

	if envGroup.Variables == nil {
		envGroup.Variables = make(map[string]string)
	}

	envGroup.Variables[key] = value
	envGroup.UpdatedAt = time.Now()

	if err := m.saveEnvGroup(envGroup); err != nil {
		return fmt.Errorf("failed to save group %s: %w", group, err)
	}

	return nil
}

// Delete deletes an environment variable from a group
func (m *Manager) Delete(group string, key string) error {
	envGroup, err := m.loadEnvGroup(group)
	if err != nil {
		return fmt.Errorf("failed to load group %s: %w", group, err)
	}

	if _, exists := envGroup.Variables[key]; !exists {
		return fmt.Errorf("variable %s not found in group %s", key, group)
	}

	delete(envGroup.Variables, key)
	envGroup.UpdatedAt = time.Now()

	if err := m.saveEnvGroup(envGroup); err != nil {
		return fmt.Errorf("failed to save group %s: %w", group, err)
	}

	return nil
}

// List lists all environment variables in a group (or all groups if group is empty)
func (m *Manager) List(group string) (map[string]map[string]string, error) {
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
				continue
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
		envGroup, err := m.loadEnvGroup(group)
		if err != nil {
			continue
		}

		for k, v := range envGroup.Variables {
			// Tolerate historical invalid keys: skip them and warn on stderr
			// so a single bad key does not break the whole export.
			if err := storage.ValidateEnvKey(k); err != nil {
				fmt.Fprintf(os.Stderr, "warning: skipping invalid env key %q in group %q (rename or delete it)\n", k, group)
				continue
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

		varCount := 0
		if err == nil {
			varCount = len(envGroup.Variables)
		}

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
