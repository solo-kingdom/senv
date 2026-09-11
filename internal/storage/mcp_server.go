package storage

import (
	"fmt"
	"strings"
	"time"
)

// MCPServerDirName is the top-level encrypted collection of user-managed MCP
// server profiles, mirroring the SSH asset and LLM provider layouts.
const MCPServerDirName = "mcp_servers"

// MCP transports accepted in profiles. stdio launches a local process; the
// remote transports (http/sse) address a server by URL. Fields are transport
// exclusive: command/args/env only on stdio, url/headers only on remote, so a
// profile can always be exported without dropping unsupported keys.
const (
	MCPTransportStdio = "stdio"
	MCPTransportHTTP  = "http"
	MCPTransportSSE   = "sse"
)

// MCPServerEntry describes one third-party MCP server that senv owns and
// exports into coding-agent configs. Env values, the remote URL and header
// values are stored as raw templates: {{env:...}} / {{text:...}} references
// are resolved at export time, never at rest.
type MCPServerEntry struct {
	Alias       string            `json:"alias"`
	Transport   string            `json:"transport"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	URL         string            `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
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
	switch e.Transport {
	case MCPTransportStdio:
		if err := validateStdioFields(e); err != nil {
			return err
		}
	case MCPTransportHTTP, MCPTransportSSE:
		if err := validateRemoteFields(e); err != nil {
			return err
		}
	default:
		return fmt.Errorf("MCP server %q: unsupported transport %q: only %q, %q, %q are supported",
			e.Alias, e.Transport, MCPTransportStdio, MCPTransportHTTP, MCPTransportSSE)
	}
	for key, value := range e.Headers {
		if err := ValidateHeaderName(key); err != nil {
			return fmt.Errorf("MCP server %q header: %w", e.Alias, err)
		}
		if err := validateHeaderValue(value); err != nil {
			return fmt.Errorf("MCP server %q header %q: %w", e.Alias, key, err)
		}
	}
	return nil
}

// validateStdioFields enforces the stdio field set: a launch command, with
// url/headers rejected so a mixed profile cannot silently mis-export.
func validateStdioFields(e *MCPServerEntry) error {
	if strings.TrimSpace(e.Command) == "" {
		return fmt.Errorf("MCP server %q: command is required", e.Alias)
	}
	if e.Command != strings.TrimSpace(e.Command) {
		return fmt.Errorf("MCP server %q: command must not have surrounding whitespace", e.Alias)
	}
	if e.URL != "" {
		return fmt.Errorf("MCP server %q: stdio transport must not set url", e.Alias)
	}
	if len(e.Headers) > 0 {
		return fmt.Errorf("MCP server %q: stdio transport must not set headers", e.Alias)
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

// validateRemoteFields enforces the http/sse field set: an http(s) URL, with
// stdio launch fields rejected because remote entries have no process to
// launch and no place to put env in any target format.
func validateRemoteFields(e *MCPServerEntry) error {
	if e.URL == "" {
		return fmt.Errorf("MCP server %q: url is required for %s transport", e.Alias, e.Transport)
	}
	lower := strings.ToLower(e.URL)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return fmt.Errorf("MCP server %q: url must be an http(s) address", e.Alias)
	}
	if strings.ContainsAny(e.URL, transportControlChars) {
		return fmt.Errorf("MCP server %q: url must not contain spaces or control characters", e.Alias)
	}
	if e.Command != "" {
		return fmt.Errorf("MCP server %q: %s transport must not set command", e.Alias, e.Transport)
	}
	if len(e.Args) > 0 {
		return fmt.Errorf("MCP server %q: %s transport must not set args", e.Alias, e.Transport)
	}
	if len(e.Env) > 0 {
		return fmt.Errorf("MCP server %q: %s transport must not set env", e.Alias, e.Transport)
	}
	return nil
}

// transportControlChars are the bytes that would break a URL or a rendered
// JSON/TOML string value on export.
const transportControlChars = "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f "

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
