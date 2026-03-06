package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wii/senv/internal/storage"
)

// Manager handles configuration file operations
type Manager struct {
	storage  *storage.Manager
	password string
}

// NewManager creates a new configuration file manager
func NewManager(storage *storage.Manager, password string) *Manager {
	return &Manager{
		storage:  storage,
		password: password,
	}
}

// Create creates a new configuration file from a source path
func (m *Manager) Create(name string, sourcePath string, targetPath string) error {
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
	if err := m.storage.SaveConfigFile(name, content, m.password); err != nil {
		return fmt.Errorf("failed to save config file: %w", err)
	}

	// Update index
	now := time.Now()
	configIndex.Configs[name] = storage.ConfigFile{
		Name:          name,
		EncryptedFile: name + storage.ConfigFileSuffix,
		TargetPath:    targetPath,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := m.storage.SaveConfigIndex(configIndex); err != nil {
		return fmt.Errorf("failed to save config index: %w", err)
	}

	return nil
}

// Edit opens a configuration file in the default editor
func (m *Manager) Edit(name string) error {
	// Check if config exists
	configIndex, err := m.storage.LoadConfigIndex()
	if err != nil {
		return fmt.Errorf("failed to load config index: %w", err)
	}

	config, exists := configIndex.Configs[name]
	if !exists {
		return fmt.Errorf("config %s not found", name)
	}

	// Load and decrypt
	content, err := m.storage.LoadConfigFile(name, m.password)
	if err != nil {
		return fmt.Errorf("failed to load config file: %w", err)
	}

	// Create temporary file
	tmpFile, err := os.CreateTemp("", "senv-config-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Ensure cleanup
	defer os.Remove(tmpPath)

	// Write content to temp file
	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	// Get editor from environment
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim" // Default to vim
	}

	// Open editor
	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run editor: %w", err)
	}

	// Read edited content
	editedContent, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to read edited file: %w", err)
	}

	// Check if content changed
	if string(editedContent) == string(content) {
		fmt.Println("No changes detected")
		return nil
	}

	// Encrypt and save
	if err := m.storage.SaveConfigFile(name, editedContent, m.password); err != nil {
		return fmt.Errorf("failed to save config file: %w", err)
	}

	// Update index
	config.UpdatedAt = time.Now()
	configIndex.Configs[name] = config

	if err := m.storage.SaveConfigIndex(configIndex); err != nil {
		return fmt.Errorf("failed to save config index: %w", err)
	}

	fmt.Printf("Config %s updated successfully\n", name)
	return nil
}

// Export exports a configuration file to a target path
func (m *Manager) Export(name string, targetPath string) error {
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

	// Expand home directory
	targetPath = expandHome(targetPath)

	// Load and decrypt
	content, err := m.storage.LoadConfigFile(name, m.password)
	if err != nil {
		return fmt.Errorf("failed to load config file: %w", err)
	}

	// Create target directory if needed
	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	// Write to target
	if err := os.WriteFile(targetPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write target file: %w", err)
	}

	fmt.Printf("Config %s exported to %s\n", name, targetPath)
	return nil
}

// Delete deletes a configuration file
func (m *Manager) Delete(name string) error {
	// Check if config exists
	configIndex, err := m.storage.LoadConfigIndex()
	if err != nil {
		return fmt.Errorf("failed to load config index: %w", err)
	}

	if _, exists := configIndex.Configs[name]; !exists {
		return fmt.Errorf("config %s not found", name)
	}

	// Delete encrypted file
	encryptedPath := filepath.Join(m.storage.GetDataPath(), name+storage.ConfigFileSuffix)
	if err := os.Remove(encryptedPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete encrypted file: %w", err)
	}

	// Update index
	delete(configIndex.Configs, name)

	if err := m.storage.SaveConfigIndex(configIndex); err != nil {
		return fmt.Errorf("failed to save config index: %w", err)
	}

	return nil
}

// List lists all configuration files
func (m *Manager) List() ([]ConfigInfo, error) {
	configIndex, err := m.storage.LoadConfigIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load config index: %w", err)
	}

	var result []ConfigInfo
	for name, config := range configIndex.Configs {
		result = append(result, ConfigInfo{
			Name:       name,
			TargetPath: config.TargetPath,
			CreatedAt:  config.CreatedAt.Format(time.RFC3339),
			UpdatedAt:  config.UpdatedAt.Format(time.RFC3339),
		})
	}

	return result, nil
}

// Get retrieves information about a specific config
func (m *Manager) Get(name string) (*ConfigInfo, error) {
	configIndex, err := m.storage.LoadConfigIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load config index: %w", err)
	}

	config, exists := configIndex.Configs[name]
	if !exists {
		return nil, fmt.Errorf("config %s not found", name)
	}

	return &ConfigInfo{
		Name:       config.Name,
		TargetPath: config.TargetPath,
		CreatedAt:  config.CreatedAt.Format(time.RFC3339),
		UpdatedAt:  config.UpdatedAt.Format(time.RFC3339),
	}, nil
}

// ConfigInfo represents information about a configuration file
type ConfigInfo struct {
	Name       string
	TargetPath string
	CreatedAt  string
	UpdatedAt  string
}

// expandHome expands ~ to the home directory
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
