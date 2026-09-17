package storage

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/securefs"
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

// deriveKeyWithIterations is a package-private seam used by tests to prove
// invalid metadata is rejected before PBKDF2. Production always uses the
// crypto implementation.
var deriveKeyWithIterations = crypto.DeriveKeyWithIterations

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
	configPath     string            // Path for configuration files (metadata, settings, etc.)
	dataPath       string            // Path for encrypted data files (env vars, config files)
	mutationLocked bool              // true only on the clone supplied by WithVaultMutation
	rekeyHooks     *rekeyHooks       // nil in production; package-private test seam
	openRoot       trustedRootOpener // package-private seam for atomic failure tests
}

// NewManager creates a new storage manager
func NewManager(configPath string, dataPath string) *Manager {
	return &Manager{
		configPath: configPath,
		dataPath:   dataPath,
		openRoot:   defaultTrustedRootOpener,
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
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.Initialize(password) })
	}
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
	key := deriveKeyWithIterations(password, salt, crypto.IterationsForNewVault())

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
	// 机器本地敏感文件（server token 等）从第一天起就被 git 同步排除
	if err := m.EnsureGitIgnoreServerToken(); err != nil {
		return fmt.Errorf("failed to write .gitignore: %w", err)
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

	if err := m.AddTextGroup("default"); err != nil {
		return fmt.Errorf("failed to create default text group: %w", err)
	}
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return err
	}
	textMeta := &EnvGroupMeta{Name: "default", CreatedAt: time.Now()}
	if err := m.SaveTextGroupMetaWithKey("default", textMeta, cryptoKey); err != nil {
		return fmt.Errorf("failed to save default text group metadata: %w", err)
	}

	if err := m.AddTextGroup("llm-keys"); err != nil {
		return fmt.Errorf("failed to create llm-keys text group: %w", err)
	}
	llmMeta := &EnvGroupMeta{
		Name:        "llm-keys",
		Description: "reserved: LLM API keys (CLI/TUI only)",
		CreatedAt:   time.Now(),
	}
	if err := m.SaveTextGroupMetaWithKey("llm-keys", llmMeta, cryptoKey); err != nil {
		return fmt.Errorf("failed to save llm-keys text group metadata: %w", err)
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

	iterations, err := metadata.ValidatedKDFIterations()
	if err != nil {
		return false, err
	}
	key := deriveKeyWithIterations(password, salt, iterations)

	passwordHash, err := crypto.Decrypt(key, metadata.PasswordKey)
	if err != nil {
		return false, nil // Password is incorrect
	}

	return crypto.HashPassword(password) == string(passwordHash), nil
}

// LoadMetadata loads the metadata file
func (m *Manager) LoadMetadata() (*Metadata, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) (*Metadata, error) { return locked.LoadMetadata() })
	}
	root, err := m.openConfigRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	data, err := root.Read(MetadataFile)
	if err != nil {
		return nil, err
	}

	metadata, err := ParseMetadata(data)
	if err != nil {
		return nil, err
	}

	return metadata, nil
}

// SaveMetadata saves the metadata file
func (m *Manager) SaveMetadata(metadata *Metadata) error {
	if _, err := metadata.ValidatedKDFIterations(); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.SaveMetadata(metadata) })
	}
	data, err := ToJSON(metadata)
	if err != nil {
		return err
	}
	root, err := m.openConfigRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.AtomicWrite([]string{MetadataFile}, data, 0o600)
}

// LoadSettings loads the settings file
func (m *Manager) LoadSettings() (*Settings, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) (*Settings, error) { return locked.LoadSettings() })
	}
	root, err := m.openConfigRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	data, err := root.Read(SettingsFile)
	if err != nil {
		return nil, err
	}

	var settings Settings
	if err := FromJSON(data, &settings); err != nil {
		return nil, err
	}
	if err := validateSettings(&settings); err != nil {
		return nil, err
	}

	return &settings, nil
}

// SaveSettings saves the settings file
func (m *Manager) SaveSettings(settings *Settings) error {
	if err := validateSettings(settings); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.SaveSettings(settings) })
	}
	data, err := ToJSON(settings)
	if err != nil {
		return err
	}
	root, err := m.openConfigRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.AtomicWrite([]string{SettingsFile}, data, 0o600)
}

// LoadEnvGroup loads an environment variable group
func (m *Manager) LoadEnvGroup(group string, password string) (*EnvGroup, error) {
	if err := ValidateName(group); err != nil {
		return nil, fmt.Errorf("invalid env group %q: %w", group, err)
	}
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
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) (*EnvGroup, error) { return locked.LoadEnvGroupWithKey(group, key) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return m.loadEnvGroupWithRoot(root, group, key)
}

// loadEnvGroupWithRoot 在共享的 data root 上加载单个分组（tui-perf-load：
// meta/变量列表/每个变量不再各自开 root）。遗留平铺格式在此路径内迁移。
func (m *Manager) loadEnvGroupWithRoot(root securefs.TrustedRoot, group string, key []byte) (*EnvGroup, error) {
	if err := ValidateName(group); err != nil {
		return nil, fmt.Errorf("invalid env group %q: %w", group, err)
	}
	_, newErr := root.ReadDir(EnvDirName, group)
	if newErr == nil {
		return m.loadEnvGroupNewFormatFromRoot(root, group, key)
	}
	if !errors.Is(newErr, os.ErrNotExist) {
		return nil, newErr
	}
	legacyName := fmt.Sprintf("%s%s%s", EnvFilePrefix, group, EnvFileSuffix)
	_, legacyErr := root.Read(legacyName)
	if legacyErr == nil {
		if _, err := m.MigrateEnvGroupIfNeeded(group, key); err != nil {
			return nil, fmt.Errorf("migration failed for group %s: %w", group, err)
		}
		return m.loadEnvGroupNewFormatFromRoot(root, group, key)
	}
	if !errors.Is(legacyErr, os.ErrNotExist) {
		return nil, legacyErr
	}
	return nil, fmt.Errorf("group %s not found: %w", group, os.ErrNotExist)
}

func (m *Manager) loadEnvGroupNewFormatFromRoot(root securefs.TrustedRoot, group string, key []byte) (*EnvGroup, error) {
	envGroup := &EnvGroup{
		Name:      group,
		Variables: make(map[string]string),
	}

	meta, err := loadEnvGroupMetaFromRoot(root, group, key)
	if err != nil {
		return nil, err
	}
	if meta.Name != group {
		return nil, fmt.Errorf("env group metadata identity mismatch: directory %q, Name %q", group, meta.Name)
	}
	envGroup.Name = meta.Name
	envGroup.CreatedAt = meta.CreatedAt
	envGroup.Description = meta.Description

	vars, err := listEnvVarsFromRoot(root, group)
	if err != nil {
		return nil, err
	}
	for _, k := range vars {
		entry, err := loadEnvVarEntryFromRoot(root, group, k, key)
		if err != nil {
			return nil, fmt.Errorf("load var %s/%s: %w", group, k, err)
		}
		envGroup.Variables[k] = entry.Value
		if entry.Description != "" {
			if envGroup.Descriptions == nil {
				envGroup.Descriptions = make(map[string]string)
			}
			envGroup.Descriptions[k] = entry.Description
		}
		envGroup.UpdatedAt = entry.UpdatedAt
	}

	return envGroup, nil
}

// LoadEnvVault loads all env groups (meta + variables) using password.
func (m *Manager) LoadEnvVault(password string) (map[string]*EnvGroup, error) {
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return nil, err
	}
	return m.LoadEnvVaultWithKey(cryptoKey)
}

// LoadEnvVaultWithKey 在单次锁内、共享单个 data root 加载全部 env 分组
// （meta + 全部变量）——一次 flock、一次 rekey 清算、一次 root 开闭，
// 替代「每分组/每变量一次」的逐文件路径。遗留平铺格式的分组透明回退到
// 逐分组加载（顺带迁移），下次进入目录列表。
func (m *Manager) LoadEnvVaultWithKey(cryptoKey []byte) (map[string]*EnvGroup, error) {
	return withVaultRead(m, func(locked *Manager) (map[string]*EnvGroup, error) {
		return locked.loadEnvVaultWithKey(cryptoKey)
	})
}

func (m *Manager) loadEnvVaultWithKey(cryptoKey []byte) (map[string]*EnvGroup, error) {
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()

	entries, err := root.ReadDir(EnvDirName)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("list env groups: %w", err)
	}
	var groups []string
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if !entry.IsDir {
			return nil, fmt.Errorf("invalid env group entry %q: expected directory", entry.Name)
		}
		if err := ValidateName(entry.Name); err != nil {
			return nil, fmt.Errorf("invalid historical env group %q: %w", entry.Name, err)
		}
		seen[entry.Name] = true
		groups = append(groups, entry.Name)
	}
	rootEntries, err := root.ReadDir()
	if err != nil {
		return nil, err
	}
	for _, entry := range rootEntries {
		if entry.IsDir || !strings.HasPrefix(entry.Name, EnvFilePrefix) || !strings.HasSuffix(entry.Name, EnvFileSuffix) {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(entry.Name, EnvFilePrefix), EnvFileSuffix)
		if err := ValidateName(name); err != nil {
			return nil, fmt.Errorf("invalid historical env group %q: %w", name, err)
		}
		if !seen[name] {
			// 遗留平铺分组：走逐分组路径（首次加载即迁移到目录格式）
			seen[name] = true
			groups = append(groups, name)
		}
	}

	out := make(map[string]*EnvGroup, len(groups))
	for _, name := range groups {
		if seenDir := func() bool {
			_, err := root.ReadDir(EnvDirName, name)
			return err == nil
		}(); seenDir {
			eg, err := m.loadEnvGroupNewFormatFromRoot(root, name, cryptoKey)
			if err != nil {
				return nil, err
			}
			out[name] = eg
			continue
		}
		eg, err := m.loadEnvGroupWithRoot(root, name, cryptoKey)
		if err != nil {
			return nil, err
		}
		out[name] = eg
	}
	return out, nil
}

// SaveEnvGroup saves an environment variable group
func (m *Manager) SaveEnvGroup(envGroup *EnvGroup, password string) error {
	if err := validateEnvGroup(envGroup); err != nil {
		return err
	}
	key, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return err
	}

	return m.SaveEnvGroupWithKey(envGroup, key)
}

// SaveEnvGroupWithKey saves an environment variable group using a derived key
func (m *Manager) SaveEnvGroupWithKey(envGroup *EnvGroup, key []byte) error {
	if err := validateEnvGroup(envGroup); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.SaveEnvGroupWithKey(envGroup, key) })
	}
	if err := m.requireCurrentKey(key); err != nil {
		return err
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	if err := root.EnsureDir([]string{EnvDirName, envGroup.Name}, 0o700); err != nil {
		root.Close()
		return err
	}
	root.Close()

	meta := &EnvGroupMeta{Name: envGroup.Name, Description: envGroup.Description, CreatedAt: envGroup.CreatedAt}
	if err := m.SaveEnvGroupMetaWithKey(envGroup.Name, meta, key); err != nil {
		return err
	}

	for k, v := range envGroup.Variables {
		entry := &EnvVarEntry{Value: v, CreatedAt: envGroup.CreatedAt, UpdatedAt: envGroup.UpdatedAt}
		if envGroup.Descriptions != nil {
			entry.Description = envGroup.Descriptions[k]
		}
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
	if err := validateEnvIdentity(group, key); err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("env entry is nil")
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.SaveEnvVarWithKey(group, key, entry, cryptoKey)
		})
	}
	if err := m.requireCurrentKey(cryptoKey); err != nil {
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
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.EnsureDir([]string{EnvDirName, group}, 0o700); err != nil {
		return err
	}
	return root.AtomicWrite([]string{EnvDirName, group, key + EnvVarSuffix}, []byte(encrypted), 0o600)
}

// SaveEnvVar saves a single environment variable using password.
func (m *Manager) SaveEnvVar(group, key string, entry *EnvVarEntry, password string) error {
	if err := validateEnvIdentity(group, key); err != nil {
		return err
	}
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return err
	}
	return m.SaveEnvVarWithKey(group, key, entry, cryptoKey)
}

// LoadEnvVarWithKey loads a single environment variable.
func (m *Manager) LoadEnvVarWithKey(group, key string, cryptoKey []byte) (*EnvVarEntry, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) (*EnvVarEntry, error) { return locked.LoadEnvVarWithKey(group, key, cryptoKey) })
	}
	if err := validateEnvIdentity(group, key); err != nil {
		return nil, err
	}
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return loadEnvVarEntryFromRoot(root, group, key, cryptoKey)
}

// loadEnvVarEntryFromRoot 在共享 root 上读取单个变量（调用方已持锁/开 root）。
func loadEnvVarEntryFromRoot(root securefs.TrustedRoot, group, key string, cryptoKey []byte) (*EnvVarEntry, error) {
	data, err := root.Read(EnvDirName, group, key+EnvVarSuffix)
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
	if err := validateEnvIdentity(group, key); err != nil {
		return nil, err
	}
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return nil, err
	}
	return m.LoadEnvVarWithKey(group, key, cryptoKey)
}

// DeleteEnvVar removes a single environment variable file.
func (m *Manager) DeleteEnvVar(group, key string) error {
	if err := validateEnvIdentity(group, key); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.DeleteEnvVar(group, key) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := removeManagedFile(root, EnvDirName, group, key+EnvVarSuffix); err != nil {
		return fmt.Errorf("failed to delete env var %q: %w", key, err)
	}
	return nil
}

// ListEnvVars lists all variable keys in a group (skips dotfiles like .meta.enc).
// Keys containing path separators are stored in subdirectories and returned
// with their relative path (e.g. "openviking/root_api_key").
func (m *Manager) ListEnvVars(group string) ([]string, error) {
	if err := ValidateName(group); err != nil {
		return nil, fmt.Errorf("invalid env group %q: %w", group, err)
	}
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return listEnvVarsFromRoot(root, group)
}

// listEnvVarsFromRoot 在共享 root 上列出分组内的变量 key（跳过 .meta.enc 等
// 点文件）；含路径分隔符的 key 以相对路径形式返回。
func listEnvVarsFromRoot(root securefs.TrustedRoot, group string) ([]string, error) {
	entries, err := root.ReadDir(EnvDirName, group)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list env vars for group %q: %w", group, err)
	}
	var keys []string
	for _, entry := range entries {
		if entry.Name == EnvMetaFileName {
			if entry.IsDir {
				return nil, fmt.Errorf("invalid env metadata entry in group %q", group)
			}
			continue
		}
		if entry.IsDir || !strings.HasSuffix(entry.Name, EnvVarSuffix) {
			return nil, fmt.Errorf("invalid historical env entry %q in group %q", entry.Name, group)
		}
		key := strings.TrimSuffix(entry.Name, EnvVarSuffix)
		if err := validateEnvIdentity(group, key); err != nil {
			return nil, fmt.Errorf("invalid historical env identity: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// SaveEnvGroupMetaWithKey saves group metadata.
func (m *Manager) SaveEnvGroupMetaWithKey(group string, meta *EnvGroupMeta, cryptoKey []byte) error {
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid env group %q: %w", group, err)
	}
	if meta == nil || meta.Name != group {
		return fmt.Errorf("env group metadata identity mismatch for %q", group)
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.SaveEnvGroupMetaWithKey(group, meta, cryptoKey)
		})
	}
	if err := m.requireCurrentKey(cryptoKey); err != nil {
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
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.EnsureDir([]string{EnvDirName, group}, 0o700); err != nil {
		return err
	}
	return root.AtomicWrite([]string{EnvDirName, group, EnvMetaFileName}, []byte(encrypted), 0o600)
}

// LoadEnvGroupMetaWithKey loads group metadata.
func (m *Manager) LoadEnvGroupMetaWithKey(group string, cryptoKey []byte) (*EnvGroupMeta, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) (*EnvGroupMeta, error) { return locked.LoadEnvGroupMetaWithKey(group, cryptoKey) })
	}
	if err := ValidateName(group); err != nil {
		return nil, fmt.Errorf("invalid env group %q: %w", group, err)
	}
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return loadEnvGroupMetaFromRoot(root, group, cryptoKey)
}

// loadEnvGroupMetaFromRoot 在共享 root 上读取分组 meta（调用方已持锁/开 root）。
func loadEnvGroupMetaFromRoot(root securefs.TrustedRoot, group string, cryptoKey []byte) (*EnvGroupMeta, error) {
	if err := ValidateName(group); err != nil {
		return nil, fmt.Errorf("invalid env group %q: %w", group, err)
	}
	data, err := root.Read(EnvDirName, group, EnvMetaFileName)
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
	if meta.Name != group {
		return nil, fmt.Errorf("env group metadata identity mismatch: requested %q, Name %q", group, meta.Name)
	}
	return &meta, nil
}

// EnvGroupExists checks whether a group exists in either new or old format.
func (m *Manager) EnvGroupExists(group string) (bool, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) (bool, error) { return locked.EnvGroupExists(group) })
	}
	if err := ValidateName(group); err != nil {
		return false, fmt.Errorf("invalid env group %q: %w", group, err)
	}
	root, err := m.openDataRoot()
	if err != nil {
		return false, err
	}
	defer root.Close()
	if _, err := root.ReadDir(EnvDirName, group); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	legacy := fmt.Sprintf("%s%s%s", EnvFilePrefix, group, EnvFileSuffix)
	if _, err := root.Read(legacy); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return false, nil
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
	iterations, err := metadata.ValidatedKDFIterations()
	if err != nil {
		return nil, err
	}
	return deriveKeyWithIterations(password, salt, iterations), nil
}

// LoadConfigIndex loads the config file index
func (m *Manager) LoadConfigIndex() (*ConfigIndex, error) {
	index, quarantined, err := m.LoadConfigIndexWithQuarantine()
	if err != nil {
		return nil, err
	}
	if len(quarantined) > 0 {
		return nil, quarantinedIndexError(quarantined)
	}
	return index, nil
}

// LoadConfigIndexWithQuarantine loads the config file index and separates
// structurally consistent legacy entries whose identities are non-portable
// (for example a ":" in the name) into the quarantine slice. Read-only
// callers may skip those entries and surface a warning; mutating callers must
// keep using LoadConfigIndex so the vault stays fail-closed until repair. A
// missing index file is an empty index rather than an error.
func (m *Manager) LoadConfigIndexWithQuarantine() (*ConfigIndex, []ConfigQuarantine, error) {
	type result struct {
		index       *ConfigIndex
		quarantined []ConfigQuarantine
	}
	loaded, err := withVaultRead(m, func(locked *Manager) (result, error) {
		root, err := locked.openConfigRoot()
		if err != nil {
			return result{}, err
		}
		defer root.Close()
		data, err := root.Read(ConfigIndexFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// A vault created before any config was stored has no index
				// file yet; treat it as an empty index so empty-vault reads
				// and the first create work without spurious errors.
				return result{index: NewConfigIndex()}, nil
			}
			return result{}, err
		}

		var configIndex ConfigIndex
		if err := FromJSON(data, &configIndex); err != nil {
			return result{}, err
		}
		index, quarantined, err := normalizeConfigIndexWithQuarantine(&configIndex)
		if err != nil {
			return result{}, err
		}
		return result{index: index, quarantined: quarantined}, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return loaded.index, loaded.quarantined, nil
}

// SaveConfigIndex saves the config file index
func (m *Manager) SaveConfigIndex(configIndex *ConfigIndex) error {
	normalized, err := normalizeConfigIndex(configIndex)
	if err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.SaveConfigIndex(normalized) })
	}
	data, err := ToJSON(normalized)
	if err != nil {
		return err
	}
	root, err := m.openConfigRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.AtomicWrite([]string{ConfigIndexFile}, data, 0o600)
}

// SaveConfigFile saves an encrypted configuration file
func (m *Manager) SaveConfigFile(name string, content []byte, password string) error {
	if err := validateConfigName(name); err != nil {
		return err
	}
	key, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return err
	}
	return m.SaveConfigFileWithKey(name, content, key)
}

// SaveConfigFileWithKey saves an encrypted configuration file using a derived key
func (m *Manager) SaveConfigFileWithKey(name string, content []byte, key []byte) error {
	if err := validateConfigName(name); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.SaveConfigFileWithKey(name, content, key)
		})
	}
	if err := m.requireCurrentKey(key); err != nil {
		return err
	}
	encryptedData, err := crypto.Encrypt(key, content)
	if err != nil {
		return err
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.AtomicWrite([]string{name + ConfigFileSuffix}, []byte(encryptedData), 0o600)
}

// LoadConfigFile loads and decrypts a configuration file
func (m *Manager) LoadConfigFile(name string, password string) ([]byte, error) {
	if err := validateConfigName(name); err != nil {
		return nil, err
	}
	key, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return nil, err
	}
	return m.LoadConfigFileWithKey(name, key)
}

// DeleteConfigFile removes one encrypted configuration file.
func (m *Manager) DeleteConfigFile(name string) error {
	if err := validateConfigName(name); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.DeleteConfigFile(name) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return removeManagedFile(root, name+ConfigFileSuffix)
}

// LoadConfigFileWithKey loads and decrypts a configuration file using a derived key
func (m *Manager) LoadConfigFileWithKey(name string, key []byte) ([]byte, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) ([]byte, error) { return locked.LoadConfigFileWithKey(name, key) })
	}
	if err := validateConfigName(name); err != nil {
		return nil, err
	}
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	encryptedData, err := root.Read(name + ConfigFileSuffix)
	if err != nil {
		return nil, err
	}
	return crypto.Decrypt(key, string(encryptedData))
}

// ListEnvGroups lists all environment variable groups.
func (m *Manager) ListEnvGroups() ([]string, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) ([]string, error) { return locked.ListEnvGroups() })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	seen := map[string]bool{}
	var groups []string
	entries, err := root.ReadDir(EnvDirName)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("list env groups: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir {
			return nil, fmt.Errorf("invalid env group entry %q: expected directory", entry.Name)
		}
		if err := ValidateName(entry.Name); err != nil {
			return nil, fmt.Errorf("invalid historical env group %q: %w", entry.Name, err)
		}
		seen[entry.Name] = true
		groups = append(groups, entry.Name)
	}
	rootEntries, err := root.ReadDir()
	if err != nil {
		return nil, err
	}
	for _, entry := range rootEntries {
		if entry.IsDir || !strings.HasPrefix(entry.Name, EnvFilePrefix) || !strings.HasSuffix(entry.Name, EnvFileSuffix) {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(entry.Name, EnvFilePrefix), EnvFileSuffix)
		if err := ValidateName(name); err != nil {
			return nil, fmt.Errorf("invalid historical env group %q: %w", name, err)
		}
		if !seen[name] {
			groups = append(groups, name)
		}
	}
	sort.Strings(groups)
	return groups, nil
}

// --- Text file storage methods ---

func (m *Manager) textFilePath(group, key string) string {
	return filepath.Join(m.dataPath, TextDirName, group, key+TextFileSuffix)
}

func (m *Manager) textGroupPath(group string) string {
	return filepath.Join(m.dataPath, TextDirName, group)
}

func (m *Manager) AddTextGroup(group string) error {
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid text group %q: %w", group, err)
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.AddTextGroup(group) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.EnsureDir([]string{TextDirName, group}, 0o700)
}

// TextGroupExists reports whether texts/{group}/ exists.
func (m *Manager) TextGroupExists(group string) (bool, error) {
	if err := ValidateName(group); err != nil {
		return false, fmt.Errorf("invalid text group %q: %w", group, err)
	}
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) (bool, error) { return locked.TextGroupExists(group) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return false, err
	}
	defer root.Close()
	_, err = root.ReadDir(TextDirName, group)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// SaveTextGroupMetaWithKey writes texts/{group}/.meta.enc.
func (m *Manager) SaveTextGroupMetaWithKey(group string, meta *EnvGroupMeta, cryptoKey []byte) error {
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid text group %q: %w", group, err)
	}
	if meta == nil || meta.Name != group {
		return fmt.Errorf("text group metadata identity mismatch for %q", group)
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.SaveTextGroupMetaWithKey(group, meta, cryptoKey)
		})
	}
	if err := m.requireCurrentKey(cryptoKey); err != nil {
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
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.EnsureDir([]string{TextDirName, group}, 0o700); err != nil {
		return err
	}
	return root.AtomicWrite([]string{TextDirName, group, EnvMetaFileName}, []byte(encrypted), 0o600)
}

// LoadTextGroupMetaWithKey loads text group metadata. Missing file returns nil, nil.
func (m *Manager) LoadTextGroupMetaWithKey(group string, cryptoKey []byte) (*EnvGroupMeta, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) (*EnvGroupMeta, error) {
			return locked.LoadTextGroupMetaWithKey(group, cryptoKey)
		})
	}
	if err := ValidateName(group); err != nil {
		return nil, fmt.Errorf("invalid text group %q: %w", group, err)
	}
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	data, err := root.Read(TextDirName, group, EnvMetaFileName)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
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
	if meta.Name != group {
		return nil, fmt.Errorf("text group metadata identity mismatch: requested %q, Name %q", group, meta.Name)
	}
	return &meta, nil
}

func (m *Manager) DeleteTextGroup(group string) error {
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid text group %q: %w", group, err)
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.DeleteTextGroup(group) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveTree(TextDirName, group)
}

func (m *Manager) SaveTextFile(group, key string, entry *TextEntry, password string) error {
	if err := validateTextIdentity(group, key); err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("text entry is nil")
	}
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return err
	}
	return m.SaveTextFileWithKey(group, key, entry, cryptoKey)
}

func (m *Manager) SaveTextFileWithKey(group, key string, entry *TextEntry, cryptoKey []byte) error {
	if err := validateTextIdentity(group, key); err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("text entry is nil")
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.SaveTextFileWithKey(group, key, entry, cryptoKey) })
	}
	if err := m.requireCurrentKey(cryptoKey); err != nil {
		return err
	}
	data, err := ToJSON(entry)
	if err != nil {
		return fmt.Errorf("failed to serialize text entry: %w", err)
	}
	encryptedData, err := crypto.Encrypt(cryptoKey, data)
	if err != nil {
		return fmt.Errorf("failed to encrypt text entry: %w", err)
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.EnsureDir([]string{TextDirName, group}, 0o700); err != nil {
		return fmt.Errorf("failed to create text group directory: %w", err)
	}
	return root.AtomicWrite([]string{TextDirName, group, key + TextFileSuffix}, []byte(encryptedData), 0o600)
}

func (m *Manager) LoadTextFile(group, key string, password string) (*TextEntry, error) {
	if err := validateTextIdentity(group, key); err != nil {
		return nil, err
	}
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return nil, err
	}
	return m.LoadTextFileWithKey(group, key, cryptoKey)
}

func (m *Manager) LoadTextFileWithKey(group, key string, cryptoKey []byte) (*TextEntry, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) (*TextEntry, error) { return locked.LoadTextFileWithKey(group, key, cryptoKey) })
	}
	if err := validateTextIdentity(group, key); err != nil {
		return nil, err
	}
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	encryptedData, err := root.Read(TextDirName, group, key+TextFileSuffix)
	if err != nil {
		return nil, fmt.Errorf("text %q not found in group %q: %w", key, group, err)
	}
	decryptedData, err := crypto.Decrypt(cryptoKey, string(encryptedData))
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt text entry: %w", err)
	}
	var entry TextEntry
	if err := FromJSON(decryptedData, &entry); err != nil {
		return nil, fmt.Errorf("failed to parse text entry: %w", err)
	}
	return &entry, nil
}

func (m *Manager) DeleteTextFile(group, key string) error {
	if err := validateTextIdentity(group, key); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.DeleteTextFile(group, key) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := removeManagedFile(root, TextDirName, group, key+TextFileSuffix); err != nil {
		return fmt.Errorf("failed to delete text %q: %w", key, err)
	}
	return nil
}

func (m *Manager) ListTextFiles(group string) ([]string, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) ([]string, error) { return locked.ListTextFiles(group) })
	}
	if err := ValidateName(group); err != nil {
		return nil, fmt.Errorf("invalid text group %q: %w", group, err)
	}
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := root.ReadDir(TextDirName, group)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list text group %q: %w", group, err)
	}
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Name == EnvMetaFileName {
			if entry.IsDir {
				return nil, fmt.Errorf("invalid text metadata entry in group %q", group)
			}
			continue
		}
		if entry.IsDir || !strings.HasSuffix(entry.Name, TextFileSuffix) {
			return nil, fmt.Errorf("invalid historical text entry %q in group %q", entry.Name, group)
		}
		key := strings.TrimSuffix(entry.Name, TextFileSuffix)
		if err := validateTextIdentity(group, key); err != nil {
			return nil, fmt.Errorf("invalid historical text identity: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func (m *Manager) ListTextGroups() ([]string, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) ([]string, error) { return locked.ListTextGroups() })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := root.ReadDir(TextDirName)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list text groups: %w", err)
	}
	groups := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir {
			return nil, fmt.Errorf("invalid text group entry %q: expected directory", entry.Name)
		}
		if err := ValidateName(entry.Name); err != nil {
			return nil, fmt.Errorf("invalid historical text group %q: %w", entry.Name, err)
		}
		groups = append(groups, entry.Name)
	}
	return groups, nil
}

// TextVaultFile 是单趟装载出的一个 text 条目：文件名解析出的 key 与解密后
// 的条目内容（TextEntry 自身不记录 key）。
type TextVaultFile struct {
	Key   string
	Entry *TextEntry
}

// TextVaultSnapshot 是单趟 vault 读内装载的全量 text 视图：全部分组（含空
// 组，Groups 保持目录枚举序）及各组解密后的条目。校验与解密语义与逐组
// ListTextGroups/ListTextFiles、逐条 LoadTextFileWithKey 完全一致（fail-closed）。
//
// 条目级失败（读取/解密/解析某个条目文件）按组降级：该组不计入 Entries、
// 原因记入 Errors，但 KeyCount 保留列出的文件数——与逐组消费方「List 失败
// → 该组置空、分组仍列出」的语义等价；目录枚举与身份校验失败仍然整体失败。
type TextVaultSnapshot struct {
	Groups   []string
	KeyCount map[string]int
	Entries  map[string][]TextVaultFile
	Errors   map[string]error
}

// LoadTextVault 使用密码加载全部 text 分组与条目（单趟锁内批量装载）。
func (m *Manager) LoadTextVault(password string) (*TextVaultSnapshot, error) {
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return nil, err
	}
	return m.LoadTextVaultWithKey(cryptoKey)
}

// LoadTextVaultWithKey 在单次 vault 读锁内、共享单个 data root 加载全部
// text 分组与条目——一次 flock、一次 rekey 清算、一次 root 开闭，替代
// 「每组一次 ListTextFiles + 每条目一次 LoadTextFile」的 N 次锁路径
// （与 LoadEnvVaultWithKey 同构，tui-startup-perf D2）。
func (m *Manager) LoadTextVaultWithKey(cryptoKey []byte) (*TextVaultSnapshot, error) {
	return withVaultRead(m, func(locked *Manager) (*TextVaultSnapshot, error) {
		return locked.loadTextVaultWithKey(cryptoKey)
	})
}

func (m *Manager) loadTextVaultWithKey(cryptoKey []byte) (*TextVaultSnapshot, error) {
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()

	groupDirs, err := root.ReadDir(TextDirName)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("list text groups: %w", err)
	}
	snap := &TextVaultSnapshot{
		KeyCount: make(map[string]int, len(groupDirs)),
		Entries:  make(map[string][]TextVaultFile, len(groupDirs)),
		Errors:   make(map[string]error),
	}
	for _, gd := range groupDirs {
		if !gd.IsDir {
			return nil, fmt.Errorf("invalid text group entry %q: expected directory", gd.Name)
		}
		if err := ValidateName(gd.Name); err != nil {
			return nil, fmt.Errorf("invalid historical text group %q: %w", gd.Name, err)
		}
		snap.Groups = append(snap.Groups, gd.Name)

		files, err := root.ReadDir(TextDirName, gd.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to list text group %q: %w", gd.Name, err)
		}
		// 文件清单与身份校验沿用逐组路径的 fail-closed 语义：任何非法
		// 历史条目使整个快照失败（对应 ListGroups 阶段失败）。组说明
		// 存在 .meta.enc，与 ListTextFiles 一样跳过，不计入条目。
		type listedFile struct {
			name string
			key  string
		}
		content := make([]listedFile, 0, len(files))
		for _, f := range files {
			if f.Name == EnvMetaFileName {
				if f.IsDir {
					return nil, fmt.Errorf("invalid text metadata entry in group %q", gd.Name)
				}
				continue
			}
			if f.IsDir || !strings.HasSuffix(f.Name, TextFileSuffix) {
				return nil, fmt.Errorf("invalid historical text entry %q in group %q", f.Name, gd.Name)
			}
			key := strings.TrimSuffix(f.Name, TextFileSuffix)
			if err := validateTextIdentity(gd.Name, key); err != nil {
				return nil, fmt.Errorf("invalid historical text identity: %w", err)
			}
			content = append(content, listedFile{name: f.Name, key: key})
		}
		snap.KeyCount[gd.Name] = len(content)
		var groupFiles []TextVaultFile
		var groupErr error
		for _, f := range content {
			encryptedData, err := root.Read(TextDirName, gd.Name, f.name)
			if err != nil {
				groupErr = fmt.Errorf("failed to read text %q in group %q: %w", f.key, gd.Name, err)
				break
			}
			decryptedData, err := crypto.Decrypt(cryptoKey, string(encryptedData))
			if err != nil {
				groupErr = fmt.Errorf("failed to decrypt text entry: %w", err)
				break
			}
			var entry TextEntry
			if err := FromJSON(decryptedData, &entry); err != nil {
				groupErr = fmt.Errorf("failed to parse text entry: %w", err)
				break
			}
			groupFiles = append(groupFiles, TextVaultFile{Key: f.key, Entry: &entry})
		}
		// 条目级失败按组降级（对应逐组 List 失败后消费方把该组置空）：分组
		// 保留列出，仅该组条目缺失。
		if groupErr != nil {
			snap.Errors[gd.Name] = groupErr
			continue
		}
		snap.Entries[gd.Name] = groupFiles
	}
	return snap, nil
}
