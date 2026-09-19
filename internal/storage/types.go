package storage

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/wii/senv/internal/crypto"
)

// Metadata represents the project metadata
type Metadata struct {
	Version       string    `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Salt          string    `json:"salt"`                     // Base64 encoded salt
	PasswordKey   string    `json:"password_key"`             // Base64 encoded encrypted password hash
	KDFIterations int       `json:"kdf_iterations,omitempty"` // PBKDF2 iterations; 0 = legacy
}

// Settings represents the user settings
type Settings struct {
	ActiveGroups []string       `json:"active_groups"`      // Groups that are activated (besides default)
	DefaultGroup string         `json:"default_group"`      // Default group name (usually "default")
	Session      SessionConfig  `json:"session"`            // Session cache configuration
	Provider     ProviderConfig `json:"provider,omitempty"` // Remote sync provider configuration (empty = git)
	UpdatedAt    string         `json:"updated_at"`
}

// ProviderConfig represents the remote sync provider configuration.
// Type empty or "git" selects the default git provider; "server" selects
// senv-server. Machine-local, never synced: the credential lives in the
// separate server-token.json (see server_token.go); the Token field below is
// legacy, read-only for migration, and MUST NOT be written by new code.
type ProviderConfig struct {
	Type    string `json:"type"`              // "git" (default) or "server"
	Address string `json:"address,omitempty"` // senv-server address
	// Token 仅为兼容旧版 settings.json 的只读字段：token 文件缺失时读取方
	// 回退到这里并迁移到 server-token.json（git 同步永远排除该凭据文件）。
	Token string `json:"token,omitempty"`
	Vault string `json:"vault,omitempty"` // vault name on server (default "main")
	// AutoSync 为 nil 时 server provider 默认开启自动同步；显式 false 关闭。
	AutoSync *bool `json:"auto_sync,omitempty"`
	// SyncThrottle 是自动 pull 的节流窗口，空值或非法值回退 30s。
	SyncThrottle string `json:"sync_throttle,omitempty"`
}

// SessionConfig represents session cache configuration
type SessionConfig struct {
	Enabled bool   `json:"enabled"` // Whether session cache is enabled
	Timeout string `json:"timeout"` // Default session timeout (e.g., "8h", "1d", "restart")
	// MaxLifetime caps sliding renewal as an absolute ceiling (default 24h).
	// An explicit --timeout larger than this is never shortened.
	MaxLifetime string `json:"max_lifetime,omitempty"`
	// AutoStart rebuilds a persistent session after a successful password
	// prompt. Off by default: password auth normally stays ephemeral.
	AutoStart bool `json:"auto_start,omitempty"`
}

// EnvGroup represents an environment variable group
type EnvGroup struct {
	Name        string            `json:"name"`
	Variables   map[string]string `json:"variables"`
	Description string            `json:"description,omitempty"`
	// Descriptions is the per-variable note map populated when loading the
	// directory format. It is not serialized on the group blob.
	Descriptions map[string]string `json:"-"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// EnvVarEntry represents a single environment variable stored in its own file.
type EnvVarEntry struct {
	Value       string    `json:"value"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// EnvGroupMeta represents group-level metadata in per-variable storage.
// Text groups reuse this shape in texts/{group}/.meta.enc.
type EnvGroupMeta struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// TextEntry represents a single text block stored in encrypted file
type TextEntry struct {
	Value       string    `json:"value"`
	Description string    `json:"description,omitempty"`
	Size        int       `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// KeyPairEntry represents an imported SSH private key. PrivateKey is always
// stored only inside an encrypted entry; PublicKey/Fingerprint are optional
// display material derived on import.
type KeyPairEntry struct {
	Name        string `json:"name"`
	PrivateKey  string `json:"private_key"`
	PublicKey   string `json:"public_key,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Comment     string `json:"comment,omitempty"`
	Description string `json:"description,omitempty"`
	// Group is the single-value group membership (empty = ungrouped),
	// semantically identical to HostEntry.Group. It is a pure organization
	// dimension: not part of materialized files or exports.
	Group      string    `json:"group,omitempty"`
	ImportedAt time.Time `json:"imported_at"`
}

// HostEntry represents an OpenSSH connection profile. IdentityKey names a
// KeyPairEntry; Extra is intentionally free-form so OpenSSH keywords can be
// passed through without senv understanding every option.
type HostEntry struct {
	Alias       string `json:"alias"`
	Hostname    string `json:"hostname,omitempty"`
	User        string `json:"user,omitempty"`
	Port        int    `json:"port,omitempty"`
	ProxyJump   string `json:"proxy_jump,omitempty"`
	IdentityKey string `json:"identity_key,omitempty"`
	// Group is the single-value group membership (empty = ungrouped). It is
	// not rendered into ssh config exports or materialized files.
	Group       string            `json:"group,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Description string            `json:"description,omitempty"`
	Extra       map[string]string `json:"extra,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// LLMProviderEntry represents a saved LLM provider profile. The credential is
// never stored here: CredentialRef points at a vault entry (for example the
// reserved text group "llm-keys") so profile metadata can be listed safely.
type LLMProviderEntry struct {
	Alias   string `json:"alias"`
	BaseURL string `json:"base_url"`
	// 形态地址（per-shape URLs）：为特定接口形态显式声明的接入地址，空值表示
	// 未声明，切换时按 ADR-0004 规则从 BaseURL 推断。chat/responses 落库走
	// OpenAI 兼容归一；anthropic 原样存储仅收敛尾斜杠（claude-code 拼
	// /v1/messages 的 base，前缀路径不可被版本段规则触碰）。
	ChatBaseURL      string `json:"chat_base_url,omitempty"`
	ResponsesBaseURL string `json:"responses_base_url,omitempty"`
	AnthropicBaseURL string `json:"anthropic_base_url,omitempty"`
	CredentialRef    string `json:"credential_ref"`
	CatalogProvider  string `json:"catalog_provider,omitempty"`
	// APIShape optionally declares the wire protocol this profile speaks
	// (openai-chat | openai-responses | anthropic). Empty keeps the legacy
	// behavior of deriving the shape from the target agent (ADR-0006).
	APIShape     string                  `json:"api_shape,omitempty"`
	Models       []string                `json:"models"`
	ModelInfo    map[string]LLMModelInfo `json:"model_info,omitempty"`
	DefaultModel string                  `json:"default_model,omitempty"`
	Description  string                  `json:"description,omitempty"`
	CreatedAt    time.Time               `json:"created_at"`
	UpdatedAt    time.Time               `json:"updated_at"`
}

// LLMModelInfo is the per-model metadata senv persists with a provider profile.
// Legacy profiles may omit it; new add/edit operations that replace the model
// set require at least ContextWindow for every model.
type LLMModelInfo struct {
	Name             string   `json:"name,omitempty"`
	Description      string   `json:"description,omitempty"`
	ContextWindow    int      `json:"context_window,omitempty"`
	OutputLimit      int      `json:"output_limit,omitempty"`
	ReasoningEfforts []string `json:"reasoning_efforts,omitempty"`
	DefaultReasoning string   `json:"default_reasoning,omitempty"`
	InputModalities  []string `json:"input_modalities,omitempty"`
}

// MaxLLMProviderModels caps the model list so a hostile catalog cannot blow up
// the entry size.
const MaxLLMProviderModels = 10000

// ValidateLLMProvider checks cross-field invariants of a provider entry.
func (e *LLMProviderEntry) ValidateLLMProvider() error {
	if e.Alias == "" {
		return fmt.Errorf("provider alias is empty")
	}
	// Existing profiles may explicitly use HTTP; load validation therefore
	// accepts both schemes but still rejects malformed URLs and userinfo.
	if err := ValidateLLMProviderURL(e.BaseURL, true); err != nil {
		return fmt.Errorf("provider %q: %w", e.Alias, err)
	}
	// 形态地址与 BaseURL 同受 URL 校验（空值合法）。逐字段检查保证错误信息
	// 指明是哪个形态地址。
	for _, field := range []struct {
		name string
		raw  string
	}{
		{"chat_base_url", e.ChatBaseURL},
		{"responses_base_url", e.ResponsesBaseURL},
		{"anthropic_base_url", e.AnthropicBaseURL},
	} {
		if field.raw == "" {
			continue
		}
		if err := ValidateLLMProviderURL(field.raw, true); err != nil {
			return fmt.Errorf("provider %q: %s: %w", e.Alias, field.name, err)
		}
	}
	if err := ValidateLLMProviderAPIShape(e.APIShape); err != nil {
		return fmt.Errorf("provider %q: %w", e.Alias, err)
	}
	if e.CredentialRef == "" {
		return fmt.Errorf("provider %q is missing credential ref", e.Alias)
	}
	if err := ValidateLLMCredentialRef(e.CredentialRef); err != nil {
		return fmt.Errorf("provider %q: %w", e.Alias, err)
	}
	if len(e.Models) == 0 {
		return fmt.Errorf("provider %q has no models", e.Alias)
	}
	if len(e.Models) > MaxLLMProviderModels {
		return fmt.Errorf("provider %q exceeds %d models", e.Alias, MaxLLMProviderModels)
	}
	if e.DefaultModel != "" && !slices.Contains(e.Models, e.DefaultModel) {
		return fmt.Errorf("provider %q default model %q is not in models", e.Alias, e.DefaultModel)
	}
	return nil
}

// MaxTextSize is the maximum allowed size for a text value (512KB)
const MaxTextSize = 512 * 1024

// MaxBackupSize is the maximum allowed size for a backup value (512KB).
// Independent of MaxTextSize so the two kinds can diverge later.
const MaxBackupSize = 512 * 1024

// BackupEntry is the decrypted JSON payload of a backup ciphertext.
// Same shape as TextEntry; a distinct name keeps call sites explicit.
type BackupEntry = TextEntry

// NewBackupEntry creates a backup payload from a value string.
func NewBackupEntry(value string) *BackupEntry {
	return NewTextEntry(value)
}

// NewTextEntry creates a new TextEntry from a value string
func NewTextEntry(value string) *TextEntry {
	now := time.Now()
	return &TextEntry{
		Value:     value,
		Size:      len(value),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// ConfigFile represents a configuration file entry
type ConfigFile struct {
	Name          string    `json:"name"`
	EncryptedFile string    `json:"encrypted_file"`        // Encrypted file name
	TargetPath    string    `json:"target_path"`           // Path to restore the file (supports ~ and env vars)
	Group         string    `json:"group,omitempty"`       // Group name; empty means "default"
	Description   string    `json:"description,omitempty"` // Human-readable description
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ConfigDefaultGroup is the group assigned when none is specified.
const ConfigDefaultGroup = "default"

// NormalizedGroup returns the effective group name (empty falls back to default).
func (c ConfigFile) NormalizedGroup() string {
	if c.Group == "" {
		return ConfigDefaultGroup
	}
	return c.Group
}

// ConfigIndex represents the config file index
type ConfigIndex struct {
	Configs map[string]ConfigFile `json:"configs"`
}

// NewMetadata creates a new Metadata instance with the current default KDF
// parameters. Callers that re-derive with a different iteration count must
// update the KDFIterations field accordingly.
func NewMetadata(salt, passwordKey string) *Metadata {
	now := time.Now()
	return &Metadata{
		Version:       currentMetadataVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
		Salt:          salt,
		PasswordKey:   passwordKey,
		KDFIterations: crypto.IterationsForNewVault(),
	}
}

// NewSettings creates a new Settings instance
func NewSettings() *Settings {
	return &Settings{
		ActiveGroups: []string{},
		DefaultGroup: "default",
		Session: SessionConfig{
			Enabled:     true,
			Timeout:     "8h",
			MaxLifetime: "24h",
		},
		Provider:  ProviderConfig{SyncThrottle: "30s"},
		UpdatedAt: time.Now().Format(time.RFC3339),
	}
}

// NewEnvGroup creates a new EnvGroup instance
func NewEnvGroup(name string) *EnvGroup {
	now := time.Now()
	return &EnvGroup{
		Name:      name,
		Variables: make(map[string]string),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewConfigIndex creates a new ConfigIndex instance
func NewConfigIndex() *ConfigIndex {
	return &ConfigIndex{
		Configs: make(map[string]ConfigFile),
	}
}

// ToJSON converts any type to JSON bytes
func ToJSON(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// FromJSON parses JSON bytes into the target
func FromJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
