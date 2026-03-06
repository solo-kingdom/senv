package storage

import (
	"encoding/json"
	"time"
)

// Metadata represents the project metadata
type Metadata struct {
	Version     string    `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Salt        string    `json:"salt"`         // Base64 encoded salt
	PasswordKey string    `json:"password_key"` // Base64 encoded encrypted password hash
}

// Settings represents the user settings
type Settings struct {
	ActiveGroups []string      `json:"active_groups"` // Groups that are activated (besides default)
	DefaultGroup string        `json:"default_group"` // Default group name (usually "default")
	Session      SessionConfig `json:"session"`       // Session cache configuration
	UpdatedAt    string        `json:"updated_at"`
}

// SessionConfig represents session cache configuration
type SessionConfig struct {
	Enabled bool   `json:"enabled"` // Whether session cache is enabled
	Timeout string `json:"timeout"` // Default session timeout (e.g., "8h", "1d", "restart", "never")
}

// EnvGroup represents an environment variable group
type EnvGroup struct {
	Name      string            `json:"name"`
	Variables map[string]string `json:"variables"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// ConfigFile represents a configuration file entry
type ConfigFile struct {
	Name          string    `json:"name"`
	EncryptedFile string    `json:"encrypted_file"` // Encrypted file name
	TargetPath    string    `json:"target_path"`    // Path to restore the file
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ConfigIndex represents the config file index
type ConfigIndex struct {
	Configs map[string]ConfigFile `json:"configs"`
}

// NewMetadata creates a new Metadata instance
func NewMetadata(salt, passwordKey string) *Metadata {
	now := time.Now()
	return &Metadata{
		Version:     "1.0",
		CreatedAt:   now,
		UpdatedAt:   now,
		Salt:        salt,
		PasswordKey: passwordKey,
	}
}

// NewSettings creates a new Settings instance
func NewSettings() *Settings {
	return &Settings{
		ActiveGroups: []string{},
		DefaultGroup: "default",
		Session: SessionConfig{
			Enabled: true,
			Timeout: "8h",
		},
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
