// Package mcp owns user-managed MCP server profiles: encrypted storage above
// the vault, and the export of those profiles into coding-agent configs.
package mcp

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/wii/senv/internal/storage"
)

// ErrExists reports an attempt to create a profile whose alias is taken. senv
// never overwrites an existing profile implicitly: an export may legitimately
// rewrite agent configs, but losing a stored definition must be explicit.
var ErrExists = errors.New("MCP server already exists")

// Server is the safe summary view used by list, TUI and MCP surfaces. It
// carries env key names but never env values.
type Server struct {
	Alias       string    `json:"alias"`
	Transport   string    `json:"transport"`
	Command     string    `json:"command"`
	Description string    `json:"description,omitempty"`
	EnvKeys     []string  `json:"env_keys,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Manager provides MCP server profile operations above encrypted storage.
type Manager struct {
	storage        *storage.Manager
	key            []byte
	password       string
	mutationLocked bool
}

// NewManager builds a password-backed manager.
func NewManager(store *storage.Manager, password string) *Manager {
	return &Manager{storage: store, password: password}
}

// NewManagerWithKey builds a manager over an already-derivated vault key.
func NewManagerWithKey(store *storage.Manager, key []byte) *Manager {
	return &Manager{storage: store, key: key}
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

func (m *Manager) load(alias string) (*storage.MCPServerEntry, error) {
	if m.key != nil {
		return m.storage.LoadMCPServerWithKey(alias, m.key)
	}
	return m.storage.LoadMCPServer(alias, m.password)
}

func (m *Manager) save(entry *storage.MCPServerEntry) error {
	if m.key != nil {
		return m.storage.SaveMCPServerWithKey(entry.Alias, entry, m.key)
	}
	return m.storage.SaveMCPServer(entry.Alias, entry, m.password)
}

// Add creates a new profile. An existing alias is rejected rather than
// replaced, so a typo cannot silently destroy a stored definition.
func (m *Manager) Add(entry *storage.MCPServerEntry) error {
	if entry == nil {
		return fmt.Errorf("MCP server entry is nil")
	}
	return m.mutate(func(locked *Manager) error {
		if _, err := locked.load(entry.Alias); err == nil {
			return fmt.Errorf("%w: %q", ErrExists, entry.Alias)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		now := time.Now().UTC()
		entry.CreatedAt = now
		entry.UpdatedAt = now
		return locked.save(entry)
	})
}

// Get returns one full profile, including env values (the CLI is the
// decryption surface; MCP callers use List instead).
func (m *Manager) Get(alias string) (*storage.MCPServerEntry, error) {
	return m.load(alias)
}

// Update applies changes in place. The alias is an identity and cannot be
// renamed here; delete and re-add instead.
func (m *Manager) Update(alias string, update func(*storage.MCPServerEntry) error) error {
	return m.mutate(func(locked *Manager) error {
		entry, err := locked.load(alias)
		if err != nil {
			return err
		}
		if update != nil {
			if err := update(entry); err != nil {
				return err
			}
		}
		if entry.Alias != alias {
			return fmt.Errorf("MCP server alias cannot be renamed from %q to %q", alias, entry.Alias)
		}
		entry.UpdatedAt = time.Now().UTC()
		return locked.save(entry)
	})
}

// Delete removes one profile. It never touches agent config files: exported
// entries are cleaned up explicitly with `senv mcp unexport`.
func (m *Manager) Delete(alias string) error {
	return m.mutate(func(locked *Manager) error {
		if _, err := locked.load(alias); err != nil {
			return err
		}
		return locked.storage.DeleteMCPServer(alias)
	})
}

// List returns profile summaries sorted by alias, with env keys visible but
// env values omitted.
func (m *Manager) List() ([]Server, error) {
	names, err := m.storage.ListMCPServers()
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	out := make([]Server, 0, len(names))
	for _, name := range names {
		entry, err := m.load(name)
		if err != nil {
			return nil, err
		}
		out = append(out, Server{
			Alias:       entry.Alias,
			Transport:   entry.Transport,
			Command:     entry.Command,
			Description: entry.Description,
			EnvKeys:     sortedEnvKeys(entry.Env),
			UpdatedAt:   entry.UpdatedAt,
		})
	}
	return out, nil
}

func sortedEnvKeys(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
