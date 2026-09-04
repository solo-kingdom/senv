package config

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wii/senv/internal/exportfile"
	"github.com/wii/senv/internal/storage"
)

// Manager handles configuration file operations
type Manager struct {
	storage        *storage.Manager
	password       string
	key            []byte
	mutationLocked bool
}

// NewManager creates a new configuration file manager
func NewManager(storage *storage.Manager, password string) *Manager {
	return &Manager{
		storage:  storage,
		password: password,
	}
}

// NewManagerWithKey creates a new configuration file manager with a derived key
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

func validateConfigName(name string) error {
	if err := storage.ValidateName(name); err != nil {
		return fmt.Errorf("invalid config name %q: %w", name, err)
	}
	return nil
}

func normalizeConfigGroup(group string) (string, error) {
	if group == "" {
		group = storage.ConfigDefaultGroup
	}
	if err := storage.ValidateName(group); err != nil {
		return "", fmt.Errorf("invalid config group %q: %w", group, err)
	}
	return group, nil
}

// QuarantineWarning reports one legacy config index entry that a read-only
// operation skipped because its identity is structurally consistent but
// non-portable (typically a ":" in the name).
type QuarantineWarning struct {
	OldName string
	Hint    string
}

func quarantineWarnings(quarantined []storage.ConfigQuarantine) []QuarantineWarning {
	if len(quarantined) == 0 {
		return nil
	}
	warnings := make([]QuarantineWarning, 0, len(quarantined))
	for _, q := range quarantined {
		warnings = append(warnings, QuarantineWarning{
			OldName: q.Name,
			Hint:    "run `senv config repair`",
		})
	}
	return warnings
}

// loadConfigFile loads a configuration file using key or password
func (m *Manager) loadConfigFile(name string) ([]byte, error) {
	if m.key != nil {
		return m.storage.LoadConfigFileWithKey(name, m.key)
	}
	return m.storage.LoadConfigFile(name, m.password)
}

// saveConfigFile saves a configuration file using key or password
func (m *Manager) saveConfigFile(name string, content []byte) error {
	if m.key != nil {
		return m.storage.SaveConfigFileWithKey(name, content, m.key)
	}
	return m.storage.SaveConfigFile(name, content, m.password)
}

// Create creates a new configuration file from a source path.
// group is optional; an empty group falls back to "default".
func (m *Manager) Create(name string, sourcePath string, targetPath string, group string, description string) error {
	if err := validateConfigName(name); err != nil {
		return err
	}
	var err error
	group, err = normalizeConfigGroup(group)
	if err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.Create(name, sourcePath, targetPath, group, description)
		})
	}
	// Check if config already exists
	configIndex, err := m.storage.LoadConfigIndex()
	if err != nil {
		return fmt.Errorf("failed to load config index: %w", err)
	}

	if _, exists := configIndex.Configs[name]; exists {
		return fmt.Errorf("config %s already exists", name)
	}

	// Read source file
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to read source file: %w", err)
	}

	// Encrypt and save
	if err := m.saveConfigFile(name, content); err != nil {
		return fmt.Errorf("failed to save config file: %w", err)
	}

	// Update index
	now := time.Now()
	configIndex.Configs[name] = storage.ConfigFile{
		Name:          name,
		EncryptedFile: name + storage.ConfigFileSuffix,
		TargetPath:    targetPath,
		Group:         group,
		Description:   description,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := m.storage.SaveConfigIndex(configIndex); err != nil {
		return fmt.Errorf("failed to save config index: %w", err)
	}

	return nil
}

// ConfigEditSession holds the state of a pending config editor invocation.
//
// The flow is split into PrepareEdit -> (run editor on TmpPath) -> FinishEdit so
// the TUI can run the editor through bubbletea's tea.ExecProcess (which
// suspends/restores the TUI) instead of blocking the program loop. The legacy
// CLI keeps using Edit which wraps both steps.
type ConfigEditSession struct {
	Name     string
	TmpPath  string
	Original []byte
}

// PrepareEdit decrypts the config into a temp file and returns the session.
func (m *Manager) PrepareEdit(name string) (*ConfigEditSession, error) {
	if err := validateConfigName(name); err != nil {
		return nil, err
	}
	configIndex, err := m.storage.LoadConfigIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load config index: %w", err)
	}
	if _, exists := configIndex.Configs[name]; !exists {
		return nil, fmt.Errorf("config %s not found", name)
	}

	content, err := m.loadConfigFile(name)
	if err != nil {
		return nil, fmt.Errorf("failed to load config file: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "senv-config-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	return &ConfigEditSession{Name: name, TmpPath: tmpPath, Original: content}, nil
}

// FinishEdit reads the edited temp file, re-encrypts when content changed,
// updates the index UpdatedAt, and removes the temp file. Returns changed=true
// when a new value was persisted.
func (m *Manager) FinishEdit(s *ConfigEditSession) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("config edit session is nil")
	}
	if err := validateConfigName(s.Name); err != nil {
		return false, err
	}
	if !m.mutationLocked {
		var changed bool
		err := m.mutate(func(locked *Manager) error {
			var err error
			changed, err = locked.FinishEdit(s)
			return err
		})
		return changed, err
	}
	defer os.Remove(s.TmpPath)

	editedContent, err := os.ReadFile(s.TmpPath)
	if err != nil {
		return false, fmt.Errorf("failed to read edited file: %w", err)
	}

	if string(editedContent) == string(s.Original) {
		return false, nil
	}

	if err := m.saveConfigFile(s.Name, editedContent); err != nil {
		return false, fmt.Errorf("failed to save config file: %w", err)
	}

	// Update index UpdatedAt.
	if configIndex, err := m.storage.LoadConfigIndex(); err == nil {
		if cfg, ok := configIndex.Configs[s.Name]; ok {
			cfg.UpdatedAt = time.Now()
			configIndex.Configs[s.Name] = cfg
			_ = m.storage.SaveConfigIndex(configIndex)
		}
	}

	return true, nil
}

// EditorCommand builds the exec.Cmd for the configured editor on the session's
// temp file, wired to the real stdio. The TUI passes this to tea.ExecProcess.
func (s *ConfigEditSession) EditorCommand() *exec.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}
	cmd := exec.Command(editor, s.TmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// Edit opens a configuration file in the default editor. It is the one-shot
// wrapper around PrepareEdit/FinishEdit used by the CLI.
func (m *Manager) Edit(name string) error {
	s, err := m.PrepareEdit(name)
	if err != nil {
		return err
	}

	if err := s.EditorCommand().Run(); err != nil {
		os.Remove(s.TmpPath)
		return fmt.Errorf("failed to run editor: %w", err)
	}

	changed, err := m.FinishEdit(s)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Println("No changes detected")
		return nil
	}

	fmt.Printf("Config %s updated successfully\n", name)
	return nil
}

// Export exports a configuration file using the private default mode.
func (m *Manager) Export(name string, targetPath string) error {
	return m.ExportWithMode(name, targetPath, exportfile.DefaultMode)
}

// ExportWithMode exports a configuration file using a mode selected for this
// operation only.
func (m *Manager) ExportWithMode(name string, targetPath string, mode fs.FileMode) error {
	if err := validateConfigName(name); err != nil {
		return err
	}
	// Check if config exists
	configIndex, err := m.storage.LoadConfigIndex()
	if err != nil {
		return fmt.Errorf("failed to load config index: %w", err)
	}

	config, exists := configIndex.Configs[name]
	if !exists {
		return fmt.Errorf("config %s not found", name)
	}

	// Use default target path if not specified
	if targetPath == "" {
		targetPath = config.TargetPath
	}

	if targetPath == "" {
		return fmt.Errorf("no target path specified and no default path configured")
	}

	// Expand ~ and environment variables
	targetPath, err = ResolveTargetPath(targetPath)
	if err != nil {
		return fmt.Errorf("failed to resolve target path: %w", err)
	}

	// Load and decrypt
	content, err := m.loadConfigFile(name)
	if err != nil {
		return fmt.Errorf("failed to load config file: %w", err)
	}

	// Shared with install: recursive mkdir, backup before overwriting a
	// differing target.
	backupPath, err := installOne(content, targetPath, mode)
	if err != nil {
		return err
	}
	if backupPath != "" {
		fmt.Printf("Existing file backed up to %s\n", backupPath)
	}
	fmt.Printf("Config %s exported to %s\n", name, targetPath)
	return nil
}

// Delete deletes a configuration file
func (m *Manager) Delete(name string) error {
	if err := validateConfigName(name); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.Delete(name) })
	}
	configIndex, err := m.storage.LoadConfigIndex()
	if err != nil {
		return fmt.Errorf("failed to load config index: %w", err)
	}
	if _, exists := configIndex.Configs[name]; !exists {
		return fmt.Errorf("config %s not found", name)
	}
	if err := m.storage.DeleteConfigFile(name); err != nil {
		return fmt.Errorf("failed to delete encrypted file: %w", err)
	}
	delete(configIndex.Configs, name)
	if err := m.storage.SaveConfigIndex(configIndex); err != nil {
		return fmt.Errorf("failed to save config index: %w", err)
	}
	return nil
}

// List lists configuration files. An empty groupFilter lists all groups.
// It fails closed when the index contains quarantined legacy entries so
// unadapted callers never silently drop them.
func (m *Manager) List(groupFilter string) ([]ConfigInfo, error) {
	if groupFilter != "" {
		if _, err := normalizeConfigGroup(groupFilter); err != nil {
			return nil, err
		}
	}
	configIndex, err := m.storage.LoadConfigIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load config index: %w", err)
	}
	return listConfigInfos(configIndex, groupFilter), nil
}

// ListWithWarnings lists configuration files and returns quarantine warnings
// for legacy entries whose non-portable identities were skipped. Warnings are
// informational only: the call still succeeds so read-only UX degrades
// gracefully instead of failing the whole listing.
func (m *Manager) ListWithWarnings(groupFilter string) ([]ConfigInfo, []QuarantineWarning, error) {
	if groupFilter != "" {
		if _, err := normalizeConfigGroup(groupFilter); err != nil {
			return nil, nil, err
		}
	}
	configIndex, quarantined, err := m.storage.LoadConfigIndexWithQuarantine()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config index: %w", err)
	}
	return listConfigInfos(configIndex, groupFilter), quarantineWarnings(quarantined), nil
}

func listConfigInfos(configIndex *storage.ConfigIndex, groupFilter string) []ConfigInfo {
	var result []ConfigInfo
	for name, config := range configIndex.Configs {
		if groupFilter != "" && config.NormalizedGroup() != groupFilter {
			continue
		}
		result = append(result, ConfigInfo{
			Name:        name,
			Group:       config.NormalizedGroup(),
			Description: config.Description,
			TargetPath:  config.TargetPath,
			CreatedAt:   config.CreatedAt.Format(time.RFC3339),
			UpdatedAt:   config.UpdatedAt.Format(time.RFC3339),
		})
	}
	return result
}

// Groups returns the sorted list of distinct group names. It fails closed when
// the index contains quarantined legacy entries.
func (m *Manager) Groups() ([]string, error) {
	configIndex, err := m.storage.LoadConfigIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load config index: %w", err)
	}
	return distinctGroups(configIndex), nil
}

// GroupsWithWarnings returns sorted distinct group names from valid index
// entries plus quarantine warnings for skipped legacy entries.
func (m *Manager) GroupsWithWarnings() ([]string, []QuarantineWarning, error) {
	configIndex, quarantined, err := m.storage.LoadConfigIndexWithQuarantine()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config index: %w", err)
	}
	return distinctGroups(configIndex), quarantineWarnings(quarantined), nil
}

func distinctGroups(configIndex *storage.ConfigIndex) []string {
	seen := map[string]bool{}
	var groups []string
	for _, config := range configIndex.Configs {
		g := config.NormalizedGroup()
		if !seen[g] {
			seen[g] = true
			groups = append(groups, g)
		}
	}
	sort.Strings(groups)
	return groups
}

// SetMeta updates the group and description of an existing config.
// An empty group falls back to "default".
func (m *Manager) SetMeta(name string, group string, description string) error {
	if err := validateConfigName(name); err != nil {
		return err
	}
	var err error
	group, err = normalizeConfigGroup(group)
	if err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.SetMeta(name, group, description) })
	}
	configIndex, err := m.storage.LoadConfigIndex()
	if err != nil {
		return fmt.Errorf("failed to load config index: %w", err)
	}
	cfg, exists := configIndex.Configs[name]
	if !exists {
		return fmt.Errorf("config %s not found", name)
	}
	cfg.Group = group
	cfg.Description = description
	cfg.UpdatedAt = time.Now()
	configIndex.Configs[name] = cfg
	return m.storage.SaveConfigIndex(configIndex)
}

// Get retrieves information about a specific config
func (m *Manager) Get(name string) (*ConfigInfo, error) {
	if err := validateConfigName(name); err != nil {
		return nil, err
	}
	configIndex, err := m.storage.LoadConfigIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load config index: %w", err)
	}

	config, exists := configIndex.Configs[name]
	if !exists {
		return nil, fmt.Errorf("config %s not found", name)
	}

	return &ConfigInfo{
		Name:        config.Name,
		Group:       config.NormalizedGroup(),
		Description: config.Description,
		TargetPath:  config.TargetPath,
		CreatedAt:   config.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   config.UpdatedAt.Format(time.RFC3339),
	}, nil
}

// ConfigInfo represents information about a configuration file
type ConfigInfo struct {
	Name        string
	Group       string
	Description string
	TargetPath  string
	CreatedAt   string
	UpdatedAt   string
}

// ResolveTargetPath expands a stored target path for use: a leading "~/" is
// expanded to the user home directory and $VAR / ${VAR} environment variables
// are expanded. Referencing an undefined variable or resolving to an empty
// path is an error so that mistakes surface before any write happens.
func ResolveTargetPath(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("target path is empty")
	}
	path := raw
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to resolve home directory: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}
	// os.ExpandEnv silently drops undefined variables; track them explicitly so
	// a typo in a stored path surfaces as an error instead of a wrong location.
	var missing []string
	path = os.Expand(path, func(name string) string {
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		missing = append(missing, name)
		return ""
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("path %q references undefined environment variable(s): %s", raw, strings.Join(missing, ", "))
	}
	if path == "" {
		return "", fmt.Errorf("path %q resolves to empty", raw)
	}
	return path, nil
}
