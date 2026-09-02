package storage

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wii/senv/internal/crypto"
)

// ErrDataDesync indicates that metadata.json and the encrypted data files do
// not share the same key (e.g. metadata was replaced via git pull while the
// data files kept the old key, or vice versa). The cmd layer uses this to
// report the real cause instead of a misleading "invalid password".
var ErrDataDesync = errors.New("metadata and encrypted data are out of sync")

// ErrOrphanedData is returned by Initialize when the data directory already
// contains encrypted files but no metadata.json exists. Initializing in this
// state would mint a brand-new key and render the existing ciphertext
// permanently undecryptable.
var ErrOrphanedData = errors.New("encrypted data files exist without metadata")

const (
	MetadataFile     = "metadata.json"
	SettingsFile     = "settings.json"
	ConfigIndexFile  = "config_index.json"
	EnvFilePrefix    = "env_"
	EnvFileSuffix    = ".json.enc"
	ConfigFileSuffix = ".enc"
	TextFileSuffix   = ".enc"
	TextDirName      = "texts"
	EnvDirName       = "envs"
	EnvVarSuffix     = ".enc"
	EnvMetaFileName  = ".meta.enc"
)

// Manager handles storage operations
type Manager struct {
	configPath string // Path for configuration files (metadata, settings, etc.)
	dataPath   string // Path for encrypted data files (env vars, config files)
}

// NewManager creates a new storage manager
func NewManager(configPath string, dataPath string) *Manager {
	return &Manager{
		configPath: configPath,
		dataPath:   dataPath,
	}
}

// GetConfigPath returns the config path
func (m *Manager) GetConfigPath() string {
	return m.configPath
}

// GetDataPath returns the data path
func (m *Manager) GetDataPath() string {
	return m.dataPath
}

// GetGitPath returns the path that should be used for git operations
// This is the common parent directory of config and data paths
func (m *Manager) GetGitPath() string {
	absConfig, err := filepath.Abs(m.configPath)
	if err != nil {
		return m.dataPath
	}
	absData, err := filepath.Abs(m.dataPath)
	if err != nil {
		return m.dataPath
	}

	// If config and data are in the same directory, use that
	configDir := filepath.Dir(absConfig)
	dataDir := filepath.Dir(absData)

	if configDir == dataDir {
		return configDir
	}

	// Otherwise find common ancestor
	for len(absConfig) > len(absData) {
		absConfig = filepath.Dir(absConfig)
	}
	for len(absData) > len(absConfig) {
		absData = filepath.Dir(absData)
	}
	for absConfig != absData {
		absConfig = filepath.Dir(absConfig)
		absData = filepath.Dir(absData)
	}

	return absConfig
}

// Initialize creates the necessary directory structure and files
func (m *Manager) Initialize(password string) error {
	// Guard against orphaned data: if encrypted files already exist but no
	// metadata is present, initializing would mint a new key and make the
	// existing ciphertext undecryptable. Refuse and explain.
	if m.HasOrphanedData() {
		return fmt.Errorf("%w: data directory %q already contains encrypted files. "+
			"Re-running init will generate a new key and make them undecryptable. "+
			"Restore metadata.json from git/version control, or back up and remove "+
			"the existing data before initializing",
			ErrOrphanedData, m.dataPath)
	}

	// Create config directory if it doesn't exist
	if err := EnsurePrivateDir(m.configPath, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Create data directory if it doesn't exist
	if err := EnsurePrivateDir(m.dataPath, 0o700); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Check if already initialized
	if m.IsInitialized() {
		return fmt.Errorf("project already initialized at %s", m.configPath)
	}

	// Generate salt
	salt, err := crypto.GenerateSalt()
	if err != nil {
		return fmt.Errorf("failed to generate salt: %w", err)
	}

	// Derive key from password with the vault's KDF parameters
	key := crypto.DeriveKeyWithIterations(password, salt, crypto.DefaultIterations)

	// Generate a verification key (encrypted hash of the password)
	passwordHash := crypto.HashPassword(password)
	passwordKey, err := crypto.Encrypt(key, []byte(passwordHash))
	if err != nil {
		return fmt.Errorf("failed to encrypt password key: %w", err)
	}

	// Create metadata
	metadata := NewMetadata(
		base64.StdEncoding.EncodeToString(salt),
		passwordKey,
	)

	// Save metadata
	if err := m.SaveMetadata(metadata); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	// Create settings
	settings := NewSettings()
	if err := m.SaveSettings(settings); err != nil {
		return fmt.Errorf("failed to save settings: %w", err)
	}

	// Create config index
	configIndex := NewConfigIndex()
	if err := m.SaveConfigIndex(configIndex); err != nil {
		return fmt.Errorf("failed to save config index: %w", err)
	}

	// Create default env group
	defaultGroup := NewEnvGroup("default")
	if err := m.SaveEnvGroup(defaultGroup, password); err != nil {
		return fmt.Errorf("failed to create default group: %w", err)
	}

	return nil
}

// IsInitialized checks if the project is initialized
func (m *Manager) IsInitialized() bool {
	metadataPath := filepath.Join(m.configPath, MetadataFile)
	_, err := os.Stat(metadataPath)
	return err == nil
}

// VerifyKey checks whether a derived key still matches the current metadata.
func (m *Manager) VerifyKey(key []byte) (bool, error) {
	if len(key) != crypto.KeySize {
		return false, nil
	}

	metadata, err := m.LoadMetadata()
	if err != nil {
		return false, fmt.Errorf("failed to load metadata: %w", err)
	}

	if _, err := crypto.Decrypt(key, metadata.PasswordKey); err != nil {
		return false, nil
	}

	return true, nil
}

// VerifyPassword verifies if the provided password is correct
func (m *Manager) VerifyPassword(password string) (bool, error) {
	metadata, err := m.LoadMetadata()
	if err != nil {
		return false, fmt.Errorf("failed to load metadata: %w", err)
	}

	salt, err := base64.StdEncoding.DecodeString(metadata.Salt)
	if err != nil {
		return false, fmt.Errorf("failed to decode salt: %w", err)
	}

	key := crypto.DeriveKeyWithIterations(password, salt, metadata.EffectiveIterations())

	passwordHash, err := crypto.Decrypt(key, metadata.PasswordKey)
	if err != nil {
		return false, nil // Password is incorrect
	}

	return crypto.HashPassword(password) == string(passwordHash), nil
}

// LoadMetadata loads the metadata file
func (m *Manager) LoadMetadata() (*Metadata, error) {
	path := filepath.Join(m.configPath, MetadataFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var metadata Metadata
	if err := FromJSON(data, &metadata); err != nil {
		return nil, err
	}

	return &metadata, nil
}

// SaveMetadata saves the metadata file
func (m *Manager) SaveMetadata(metadata *Metadata) error {
	path := filepath.Join(m.configPath, MetadataFile)
	data, err := ToJSON(metadata)
	if err != nil {
		return err
	}

	return WriteSensitiveFile(path, data, 0o700, 0o600)
}

// LoadSettings loads the settings file
func (m *Manager) LoadSettings() (*Settings, error) {
	path := filepath.Join(m.configPath, SettingsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var settings Settings
	if err := FromJSON(data, &settings); err != nil {
		return nil, err
	}

	return &settings, nil
}

// SaveSettings saves the settings file
func (m *Manager) SaveSettings(settings *Settings) error {
	path := filepath.Join(m.configPath, SettingsFile)
	data, err := ToJSON(settings)
	if err != nil {
		return err
	}

	return WriteSensitiveFile(path, data, 0o700, 0o600)
}

// LoadEnvGroup loads an environment variable group
func (m *Manager) LoadEnvGroup(group string, password string) (*EnvGroup, error) {
	key, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return nil, err
	}

	return m.LoadEnvGroupWithKey(group, key)
}

// LoadEnvGroupWithKey loads an environment variable group using a derived key.
// It reads from the new per-variable format, transparently migrating old-format
// files on first access.
func (m *Manager) LoadEnvGroupWithKey(group string, key []byte) (*EnvGroup, error) {
	groupDir := m.envGroupDir(group)
	if info, err := os.Stat(groupDir); err == nil && info.IsDir() {
		return m.loadEnvGroupNewFormat(group, key)
	}

	oldPath := filepath.Join(m.dataPath, fmt.Sprintf("%s%s%s", EnvFilePrefix, group, EnvFileSuffix))
	if _, err := os.Stat(oldPath); err == nil {
		if _, err := m.MigrateEnvGroupIfNeeded(group, key); err != nil {
			return nil, fmt.Errorf("migration failed for group %s: %w", group, err)
		}
		return m.loadEnvGroupNewFormat(group, key)
	}

	return nil, fmt.Errorf("group %s not found", group)
}

func (m *Manager) loadEnvGroupNewFormat(group string, key []byte) (*EnvGroup, error) {
	envGroup := &EnvGroup{
		Name:      group,
		Variables: make(map[string]string),
	}

	if meta, err := m.LoadEnvGroupMetaWithKey(group, key); err == nil {
		envGroup.Name = meta.Name
		envGroup.CreatedAt = meta.CreatedAt
	}

	vars, err := m.ListEnvVars(group)
	if err != nil {
		return nil, err
	}
	for _, k := range vars {
		entry, err := m.LoadEnvVarWithKey(group, k, key)
		if err != nil {
			return nil, fmt.Errorf("load var %s/%s: %w", group, k, err)
		}
		envGroup.Variables[k] = entry.Value
		envGroup.UpdatedAt = entry.UpdatedAt
	}

	return envGroup, nil
}

// SaveEnvGroup saves an environment variable group
func (m *Manager) SaveEnvGroup(envGroup *EnvGroup, password string) error {
	key, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return err
	}

	return m.SaveEnvGroupWithKey(envGroup, key)
}

// SaveEnvGroupWithKey saves an environment variable group using a derived key
func (m *Manager) SaveEnvGroupWithKey(envGroup *EnvGroup, key []byte) error {
	groupDir := m.envGroupDir(envGroup.Name)
	if err := EnsurePrivateDir(groupDir, 0o700); err != nil {
		return err
	}

	meta := &EnvGroupMeta{Name: envGroup.Name, CreatedAt: envGroup.CreatedAt}
	if err := m.SaveEnvGroupMetaWithKey(envGroup.Name, meta, key); err != nil {
		return err
	}

	for k, v := range envGroup.Variables {
		entry := &EnvVarEntry{Value: v, CreatedAt: envGroup.CreatedAt, UpdatedAt: envGroup.UpdatedAt}
		if err := m.SaveEnvVarWithKey(envGroup.Name, k, entry, key); err != nil {
			return err
		}
	}
	return nil
}

// --- Per-variable env storage methods ---

func (m *Manager) envGroupDir(group string) string {
	return filepath.Join(m.dataPath, EnvDirName, group)
}

func (m *Manager) envVarPath(group, key string) string {
	return filepath.Join(m.dataPath, EnvDirName, group, key+EnvVarSuffix)
}

func (m *Manager) envMetaPath(group string) string {
	return filepath.Join(m.dataPath, EnvDirName, group, EnvMetaFileName)
}

// SaveEnvVarWithKey saves a single environment variable.
func (m *Manager) SaveEnvVarWithKey(group, key string, entry *EnvVarEntry, cryptoKey []byte) error {
	varPath := m.envVarPath(group, key)
	if err := EnsurePrivateDir(filepath.Dir(varPath), 0o700); err != nil {
		return err
	}

	data, err := ToJSON(entry)
	if err != nil {
		return err
	}
	encrypted, err := crypto.Encrypt(cryptoKey, data)
	if err != nil {
		return err
	}
	return WriteSensitiveFile(varPath, []byte(encrypted), 0o700, 0o600)
}

// SaveEnvVar saves a single environment variable using password.
func (m *Manager) SaveEnvVar(group, key string, entry *EnvVarEntry, password string) error {
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return err
	}
	return m.SaveEnvVarWithKey(group, key, entry, cryptoKey)
}

// LoadEnvVarWithKey loads a single environment variable.
func (m *Manager) LoadEnvVarWithKey(group, key string, cryptoKey []byte) (*EnvVarEntry, error) {
	data, err := os.ReadFile(m.envVarPath(group, key))
	if err != nil {
		return nil, err
	}
	decrypted, err := crypto.Decrypt(cryptoKey, string(data))
	if err != nil {
		return nil, err
	}
	var entry EnvVarEntry
	if err := FromJSON(decrypted, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// LoadEnvVar loads a single environment variable using password.
func (m *Manager) LoadEnvVar(group, key string, password string) (*EnvVarEntry, error) {
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return nil, err
	}
	return m.LoadEnvVarWithKey(group, key, cryptoKey)
}

// DeleteEnvVar removes a single environment variable file.
func (m *Manager) DeleteEnvVar(group, key string) error {
	path := m.envVarPath(group, key)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete env var '%s': %w", key, err)
	}
	return nil
}

// ListEnvVars lists all variable keys in a group (skips dotfiles like .meta.enc).
// Keys containing path separators are stored in subdirectories and returned
// with their relative path (e.g. "openviking/root_api_key").
func (m *Manager) ListEnvVars(group string) ([]string, error) {
	groupDir := m.envGroupDir(group)
	if _, err := os.Stat(groupDir); os.IsNotExist(err) {
		return nil, nil
	}

	var keys []string
	err := filepath.WalkDir(groupDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".") || !strings.HasSuffix(d.Name(), EnvVarSuffix) {
			return nil
		}
		rel, err := filepath.Rel(groupDir, path)
		if err != nil {
			return nil
		}
		keys = append(keys, strings.TrimSuffix(rel, EnvVarSuffix))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list env vars for group '%s': %w", group, err)
	}
	return keys, nil
}

// SaveEnvGroupMetaWithKey saves group metadata.
func (m *Manager) SaveEnvGroupMetaWithKey(group string, meta *EnvGroupMeta, cryptoKey []byte) error {
	groupDir := m.envGroupDir(group)
	if err := EnsurePrivateDir(groupDir, 0o700); err != nil {
		return err
	}
	data, err := ToJSON(meta)
	if err != nil {
		return err
	}
	encrypted, err := crypto.Encrypt(cryptoKey, data)
	if err != nil {
		return err
	}
	return WriteSensitiveFile(m.envMetaPath(group), []byte(encrypted), 0o700, 0o600)
}

// LoadEnvGroupMetaWithKey loads group metadata.
func (m *Manager) LoadEnvGroupMetaWithKey(group string, cryptoKey []byte) (*EnvGroupMeta, error) {
	data, err := os.ReadFile(m.envMetaPath(group))
	if err != nil {
		return nil, err
	}
	decrypted, err := crypto.Decrypt(cryptoKey, string(data))
	if err != nil {
		return nil, err
	}
	var meta EnvGroupMeta
	if err := FromJSON(decrypted, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// EnvGroupExists checks whether a group exists in either new or old format.
func (m *Manager) EnvGroupExists(group string) bool {
	if info, err := os.Stat(m.envGroupDir(group)); err == nil && info.IsDir() {
		return true
	}
	oldPath := filepath.Join(m.dataPath, fmt.Sprintf("%s%s%s", EnvFilePrefix, group, EnvFileSuffix))
	if _, err := os.Stat(oldPath); err == nil {
		return true
	}
	return false
}

func (m *Manager) deriveKeyFromPassword(password string) ([]byte, error) {
	metadata, err := m.LoadMetadata()
	if err != nil {
		return nil, err
	}
	salt, err := base64.StdEncoding.DecodeString(metadata.Salt)
	if err != nil {
		return nil, err
	}
	return crypto.DeriveKeyWithIterations(password, salt, metadata.EffectiveIterations()), nil
}

// LoadConfigIndex loads the config file index
func (m *Manager) LoadConfigIndex() (*ConfigIndex, error) {
	path := filepath.Join(m.configPath, ConfigIndexFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var configIndex ConfigIndex
	if err := FromJSON(data, &configIndex); err != nil {
		return nil, err
	}

	return &configIndex, nil
}

// SaveConfigIndex saves the config file index
func (m *Manager) SaveConfigIndex(configIndex *ConfigIndex) error {
	path := filepath.Join(m.configPath, ConfigIndexFile)
	data, err := ToJSON(configIndex)
	if err != nil {
		return err
	}

	return WriteSensitiveFile(path, data, 0o700, 0o600)
}

// SaveConfigFile saves an encrypted configuration file
func (m *Manager) SaveConfigFile(name string, content []byte, password string) error {
	key, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return err
	}
	return m.SaveConfigFileWithKey(name, content, key)
}

// SaveConfigFileWithKey saves an encrypted configuration file using a derived key
func (m *Manager) SaveConfigFileWithKey(name string, content []byte, key []byte) error {
	encryptedData, err := crypto.Encrypt(key, content)
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("%s%s", name, ConfigFileSuffix)
	path := filepath.Join(m.dataPath, filename)

	return WriteSensitiveFile(path, []byte(encryptedData), 0o700, 0o600)
}

// LoadConfigFile loads and decrypts a configuration file
func (m *Manager) LoadConfigFile(name string, password string) ([]byte, error) {
	key, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return nil, err
	}
	return m.LoadConfigFileWithKey(name, key)
}

// LoadConfigFileWithKey loads and decrypts a configuration file using a derived key
func (m *Manager) LoadConfigFileWithKey(name string, key []byte) ([]byte, error) {
	filename := fmt.Sprintf("%s%s", name, ConfigFileSuffix)
	path := filepath.Join(m.dataPath, filename)

	encryptedData, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return crypto.Decrypt(key, string(encryptedData))
}

// ListEnvGroups lists all environment variable groups
func (m *Manager) ListEnvGroups() ([]string, error) {
	seen := map[string]bool{}
	var groups []string

	envsDir := filepath.Join(m.dataPath, EnvDirName)
	if entries, err := os.ReadDir(envsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				seen[e.Name()] = true
				groups = append(groups, e.Name())
			}
		}
	}

	files, err := os.ReadDir(m.dataPath)
	if err != nil {
		if len(groups) > 0 {
			return groups, nil
		}
		return nil, err
	}
	for _, file := range files {
		if strings.HasPrefix(file.Name(), EnvFilePrefix) && strings.HasSuffix(file.Name(), EnvFileSuffix) {
			name := strings.TrimPrefix(file.Name(), EnvFilePrefix)
			name = strings.TrimSuffix(name, EnvFileSuffix)
			if !seen[name] {
				groups = append(groups, name)
			}
		}
	}

	return groups, nil
}

// --- Text file storage methods ---

// textFilePath returns the full path for a text entry file
func (m *Manager) textFilePath(group, key string) string {
	return filepath.Join(m.dataPath, TextDirName, group, key+TextFileSuffix)
}

// textGroupPath returns the full path for a text group directory
func (m *Manager) textGroupPath(group string) string {
	return filepath.Join(m.dataPath, TextDirName, group)
}

// SaveTextFile saves an encrypted text entry
func (m *Manager) SaveTextFile(group, key string, entry *TextEntry, password string) error {
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return err
	}
	return m.SaveTextFileWithKey(group, key, entry, cryptoKey)
}

// SaveTextFileWithKey saves an encrypted text entry using a derived key
func (m *Manager) SaveTextFileWithKey(group, key string, entry *TextEntry, cryptoKey []byte) error {
	// Ensure group directory exists
	groupDir := m.textGroupPath(group)
	if err := EnsurePrivateDir(groupDir, 0o700); err != nil {
		return fmt.Errorf("failed to create text group directory: %w", err)
	}

	// Serialize to JSON
	data, err := ToJSON(entry)
	if err != nil {
		return fmt.Errorf("failed to serialize text entry: %w", err)
	}

	// Encrypt
	encryptedData, err := crypto.Encrypt(cryptoKey, data)
	if err != nil {
		return fmt.Errorf("failed to encrypt text entry: %w", err)
	}

	// Write to file
	path := m.textFilePath(group, key)
	return WriteSensitiveFile(path, []byte(encryptedData), 0o700, 0o600)
}

// LoadTextFile loads and decrypts a text entry
func (m *Manager) LoadTextFile(group, key string, password string) (*TextEntry, error) {
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return nil, err
	}
	return m.LoadTextFileWithKey(group, key, cryptoKey)
}

// LoadTextFileWithKey loads and decrypts a text entry using a derived key
func (m *Manager) LoadTextFileWithKey(group, key string, cryptoKey []byte) (*TextEntry, error) {
	path := m.textFilePath(group, key)

	encryptedData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("text '%s' not found in group '%s': %w", key, group, err)
	}

	// Decrypt
	decryptedData, err := crypto.Decrypt(cryptoKey, string(encryptedData))
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt text entry: %w", err)
	}

	// Parse JSON
	var entry TextEntry
	if err := FromJSON(decryptedData, &entry); err != nil {
		return nil, fmt.Errorf("failed to parse text entry: %w", err)
	}

	return &entry, nil
}

// DeleteTextFile deletes a text entry file
func (m *Manager) DeleteTextFile(group, key string) error {
	path := m.textFilePath(group, key)

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete text '%s': %w", key, err)
	}

	return nil
}

// ListTextFiles lists all text keys in a group
func (m *Manager) ListTextFiles(group string) ([]string, error) {
	groupDir := m.textGroupPath(group)

	entries, err := os.ReadDir(groupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list text group '%s': %w", group, err)
	}

	var keys []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), TextFileSuffix) {
			name := strings.TrimSuffix(entry.Name(), TextFileSuffix)
			keys = append(keys, name)
		}
	}

	return keys, nil
}

// ListTextGroups lists all text group directories
func (m *Manager) ListTextGroups() ([]string, error) {
	textsDir := filepath.Join(m.dataPath, TextDirName)

	entries, err := os.ReadDir(textsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list text groups: %w", err)
	}

	var groups []string
	for _, entry := range entries {
		if entry.IsDir() {
			groups = append(groups, entry.Name())
		}
	}

	return groups, nil
}
