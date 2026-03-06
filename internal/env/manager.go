package env

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wii/senv/internal/storage"
)

// Manager handles environment variable operations
type Manager struct {
	storage  *storage.Manager
	password string
	key      []byte // Derived key (alternative to password)
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

// Get retrieves an environment variable from a group
func (m *Manager) Get(group string, key string) (string, error) {
	var envGroup *storage.EnvGroup
	var err error

	if m.key != nil {
		envGroup, err = m.storage.LoadEnvGroupWithKey(group, m.key)
	} else {
		envGroup, err = m.storage.LoadEnvGroup(group, m.password)
	}

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
	var envGroup *storage.EnvGroup
	var err error

	if m.key != nil {
		envGroup, err = m.storage.LoadEnvGroupWithKey(group, m.key)
	} else {
		envGroup, err = m.storage.LoadEnvGroup(group, m.password)
	}

	if err != nil {
		// If group doesn't exist, create it
		envGroup = storage.NewEnvGroup(group)
	}

	if envGroup.Variables == nil {
		envGroup.Variables = make(map[string]string)
	}

	envGroup.Variables[key] = value
	envGroup.UpdatedAt = time.Now()

	if m.key != nil {
		err = m.storage.SaveEnvGroupWithKey(envGroup, m.key)
	} else {
		err = m.storage.SaveEnvGroup(envGroup, m.password)
	}

	if err != nil {
		return fmt.Errorf("failed to save group %s: %w", group, err)
	}

	return nil
}

// Delete deletes an environment variable from a group
func (m *Manager) Delete(group string, key string) error {
	var envGroup *storage.EnvGroup
	var err error

	if m.key != nil {
		envGroup, err = m.storage.LoadEnvGroupWithKey(group, m.key)
	} else {
		envGroup, err = m.storage.LoadEnvGroup(group, m.password)
	}

	if err != nil {
		return fmt.Errorf("failed to load group %s: %w", group, err)
	}

	if _, exists := envGroup.Variables[key]; !exists {
		return fmt.Errorf("variable %s not found in group %s", key, group)
	}

	delete(envGroup.Variables, key)
	envGroup.UpdatedAt = time.Now()

	if m.key != nil {
		err = m.storage.SaveEnvGroupWithKey(envGroup, m.key)
	} else {
		err = m.storage.SaveEnvGroup(envGroup, m.password)
	}

	if err != nil {
		return fmt.Errorf("failed to save group %s: %w", group, err)
	}

	return nil
}

// List lists all environment variables in a group (or all groups if group is empty)
func (m *Manager) List(group string) (map[string]map[string]string, error) {
	result := make(map[string]map[string]string)

	if group != "" {
		// List specific group
		var envGroup *storage.EnvGroup
		var err error

		if m.key != nil {
			envGroup, err = m.storage.LoadEnvGroupWithKey(group, m.key)
		} else {
			envGroup, err = m.storage.LoadEnvGroup(group, m.password)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to load group %s: %w", group, err)
		}
		result[group] = envGroup.Variables
	} else {
		// List all groups
		groups, err := m.storage.ListEnvGroups()
		if err != nil {
			return nil, fmt.Errorf("failed to list groups: %w", err)
		}

		for _, g := range groups {
			var envGroup *storage.EnvGroup
			if m.key != nil {
				envGroup, err = m.storage.LoadEnvGroupWithKey(g, m.key)
			} else {
				envGroup, err = m.storage.LoadEnvGroup(g, m.password)
			}
			if err != nil {
				continue // Skip groups that can't be loaded
			}
			result[g] = envGroup.Variables
		}
	}

	return result, nil
}

// Export exports environment variables from active groups
func (m *Manager) Export() (string, error) {
	// Load settings to get active groups
	settings, err := m.storage.LoadSettings()
	if err != nil {
		return "", fmt.Errorf("failed to load settings: %w", err)
	}

	// Always include default group
	activeGroups := []string{settings.DefaultGroup}

	// Add other active groups
	for _, g := range settings.ActiveGroups {
		if g != settings.DefaultGroup {
			activeGroups = append(activeGroups, g)
		}
	}

	// Collect all variables
	allVars := make(map[string]string)
	for _, group := range activeGroups {
		var envGroup *storage.EnvGroup
		if m.key != nil {
			envGroup, err = m.storage.LoadEnvGroupWithKey(group, m.key)
		} else {
			envGroup, err = m.storage.LoadEnvGroup(group, m.password)
		}
		if err != nil {
			continue // Skip groups that can't be loaded
		}

		for k, v := range envGroup.Variables {
			allVars[k] = v
		}
	}

	// Generate export statements
	var lines []string

	// Sort keys for consistent output
	keys := make([]string, 0, len(allVars))
	for k := range allVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := allVars[key]
		// Escape single quotes in value
		escapedValue := strings.ReplaceAll(value, "'", "'\\''")
		lines = append(lines, fmt.Sprintf("export %s='%s'", key, escapedValue))
	}

	return strings.Join(lines, "\n"), nil
}

// AddGroup creates a new environment variable group
func (m *Manager) AddGroup(name string) error {
	// Check if group already exists
	groups, err := m.storage.ListEnvGroups()
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}

	for _, g := range groups {
		if g == name {
			return fmt.Errorf("group %s already exists", name)
		}
	}

	// Create new group
	envGroup := storage.NewEnvGroup(name)
	if m.key != nil {
		err = m.storage.SaveEnvGroupWithKey(envGroup, m.key)
	} else {
		err = m.storage.SaveEnvGroup(envGroup, m.password)
	}

	if err != nil {
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

	// Check if group exists
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

	// Don't add default group (it's always active)
	if name == settings.DefaultGroup {
		return nil
	}

	// Check if already active
	for _, g := range settings.ActiveGroups {
		if g == name {
			return nil // Already active
		}
	}

	// Add to active groups
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

	// Can't deactivate default group
	if name == settings.DefaultGroup {
		return fmt.Errorf("cannot deactivate default group")
	}

	// Remove from active groups
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

		var envGroup *storage.EnvGroup
		if m.key != nil {
			envGroup, err = m.storage.LoadEnvGroupWithKey(name, m.key)
		} else {
			envGroup, err = m.storage.LoadEnvGroup(name, m.password)
		}

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
