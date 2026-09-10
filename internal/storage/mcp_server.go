package storage

import (
	"fmt"
	"strings"
	"time"
)

// MCPServerDirName is the top-level encrypted collection of user-managed MCP
// server profiles, mirroring the SSH asset and LLM provider layouts.
const MCPServerDirName = "mcp_servers"

// MCPTransportStdio is the only transport supported in V1. Remote transports
// (http/sse) use agent-specific config keys and are deliberately deferred, so
// a profile that declares one is rejected rather than silently mis-exported.
const MCPTransportStdio = "stdio"

// MCPServerEntry describes one third-party MCP server that senv owns and
// exports into coding-agent configs. Env values are stored as raw templates:
// {{env:...}} / {{text:...}} references are resolved at export time, never at
// rest.
type MCPServerEntry struct {
	Alias       string            `json:"alias"`
	Transport   string            `json:"transport"`
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Description string            `json:"description,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// ValidateMCPServer checks cross-field invariants. It runs on save (to reject
// bad input early) and on load: decryption alone is not authorization to hand
// a malformed profile to an exporter or an MCP client.
func (e *MCPServerEntry) ValidateMCPServer() error {
	if e.Alias == "" {
		return fmt.Errorf("MCP server alias is empty")
	}
	if err := validateEntryIdentity("MCP server", e.Alias); err != nil {
		return err
	}
	if e.Transport != MCPTransportStdio {
		return fmt.Errorf("MCP server %q: unsupported transport %q: only %q is supported", e.Alias, e.Transport, MCPTransportStdio)
	}
	if strings.TrimSpace(e.Command) == "" {
		return fmt.Errorf("MCP server %q: command is required", e.Alias)
	}
	if e.Command != strings.TrimSpace(e.Command) {
		return fmt.Errorf("MCP server %q: command must not have surrounding whitespace", e.Alias)
	}
	for _, arg := range e.Args {
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("MCP server %q: args must not contain NUL bytes", e.Alias)
		}
	}
	for key, value := range e.Env {
		// Env keys become config-file keys in every target format, so they must
		// stay shell-variable shaped; anything else could render invalid TOML.
		if err := ValidateEnvKey(key); err != nil {
			return fmt.Errorf("MCP server %q env: %w", e.Alias, err)
		}
		if strings.ContainsRune(value, 0) {
			return fmt.Errorf("MCP server %q env %q: value must not contain NUL bytes", e.Alias, key)
		}
	}
	return nil
}

// SaveMCPServerWithKey validates and atomically writes an encrypted MCP server
// profile.
func (m *Manager) SaveMCPServerWithKey(alias string, entry *MCPServerEntry, cryptoKey []byte) error {
	if err := validateEntryIdentity("MCP server", alias); err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("MCP server entry is nil")
	}
	if entry.Alias != alias {
		return fmt.Errorf("MCP server alias %q does not match entry alias %q", alias, entry.Alias)
	}
	if err := entry.ValidateMCPServer(); err != nil {
		return err
	}
	return m.saveSSHEntry(MCPServerDirName, alias, entry, cryptoKey)
}

// LoadMCPServerWithKey loads and decrypts one MCP server profile.
func (m *Manager) LoadMCPServerWithKey(alias string, cryptoKey []byte) (*MCPServerEntry, error) {
	var entry MCPServerEntry
	if err := m.loadSSHEntry(MCPServerDirName, alias, &entry, cryptoKey); err != nil {
		return nil, err
	}
	if err := entry.ValidateMCPServer(); err != nil {
		return nil, fmt.Errorf("invalid MCP server %q: %w", alias, err)
	}
	return &entry, nil
}

// SaveMCPServer derives the vault key from password and saves a profile.
func (m *Manager) SaveMCPServer(alias string, entry *MCPServerEntry, password string) error {
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return err
	}
	return m.SaveMCPServerWithKey(alias, entry, cryptoKey)
}

// LoadMCPServer derives the vault key from password and loads a profile.
func (m *Manager) LoadMCPServer(alias string, password string) (*MCPServerEntry, error) {
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return nil, err
	}
	return m.LoadMCPServerWithKey(alias, cryptoKey)
}

// DeleteMCPServer removes one profile. A missing alias is not an error,
// matching the existing idempotent entry deletion behavior.
func (m *Manager) DeleteMCPServer(alias string) error {
	return m.deleteSSHEntry(MCPServerDirName, alias, "MCP server")
}

// ListMCPServers returns all valid MCP server aliases, sorted by the trusted
// root.
func (m *Manager) ListMCPServers() ([]string, error) {
	return m.listSSHEntries(MCPServerDirName)
}
