package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wii/senv/internal/crypto"
)

const (
	// HostDirName and KeypairDirName are top-level encrypted data collections.
	HostDirName    = "hosts"
	KeypairDirName = "keypairs"
)

func validateSSHIdentity(kind, name string) error {
	return wrapInvalidIdentity("invalid SSH "+kind, name)
}

// validateEntryIdentity is the kind-neutral variant used by shared entry
// helpers so non-SSH collections render accurate error messages.
func validateEntryIdentity(kind, name string) error {
	return wrapInvalidIdentity("invalid "+kind, name)
}

func wrapInvalidIdentity(prefix, name string) error {
	if err := ValidateName(name); err != nil {
		return fmt.Errorf("%s %q: %w", prefix, name, err)
	}
	return nil
}

// hostFilePath returns the path used by tests and diagnostics; production I/O
// goes through the trusted data root.
func (m *Manager) hostFilePath(alias string) string {
	return filepath.Join(m.dataPath, HostDirName, alias+ConfigFileSuffix)
}

// keypairFilePath returns the path used by tests and diagnostics; production
// I/O goes through the trusted data root.
func (m *Manager) keypairFilePath(name string) string {
	return filepath.Join(m.dataPath, KeypairDirName, name+ConfigFileSuffix)
}

// SaveHostWithKey validates and atomically writes an encrypted host entry.
func (m *Manager) SaveHostWithKey(alias string, entry *HostEntry, cryptoKey []byte) error {
	if err := validateSSHIdentity("host", alias); err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("host entry is nil")
	}
	if entry.Alias != alias {
		return fmt.Errorf("host alias %q does not match entry alias %q", alias, entry.Alias)
	}
	return m.saveSSHEntry(HostDirName, alias, entry, cryptoKey)
}

// SaveHost derives the vault key from password and saves a host entry.
func (m *Manager) SaveHost(alias string, entry *HostEntry, password string) error {
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return err
	}
	return m.SaveHostWithKey(alias, entry, cryptoKey)
}

// LoadHostWithKey loads and decrypts one host entry.
func (m *Manager) LoadHostWithKey(alias string, cryptoKey []byte) (*HostEntry, error) {
	var entry HostEntry
	if err := m.loadSSHEntry(HostDirName, alias, &entry, cryptoKey); err != nil {
		return nil, err
	}
	return &entry, nil
}

// LoadHost derives the vault key from password and loads a host entry.
func (m *Manager) LoadHost(alias string, password string) (*HostEntry, error) {
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return nil, err
	}
	return m.LoadHostWithKey(alias, cryptoKey)
}

// DeleteHost removes one host entry. A missing alias is not an error, matching
// the existing idempotent text deletion behavior.
func (m *Manager) DeleteHost(alias string) error {
	return m.deleteSSHEntry(HostDirName, alias, "host")
}

// ListHosts returns all valid host aliases, sorted by the trusted root.
func (m *Manager) ListHosts() ([]string, error) {
	return m.listSSHEntries(HostDirName)
}

// SaveKeyPairWithKey validates and atomically writes an encrypted keypair.
func (m *Manager) SaveKeyPairWithKey(name string, entry *KeyPairEntry, cryptoKey []byte) error {
	if err := validateSSHIdentity("keypair", name); err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("keypair entry is nil")
	}
	if entry.Name != name {
		return fmt.Errorf("keypair name %q does not match entry name %q", name, entry.Name)
	}
	return m.saveSSHEntry(KeypairDirName, name, entry, cryptoKey)
}

// SaveKeyPair derives the vault key from password and saves a keypair.
func (m *Manager) SaveKeyPair(name string, entry *KeyPairEntry, password string) error {
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return err
	}
	return m.SaveKeyPairWithKey(name, entry, cryptoKey)
}

// LoadKeyPairWithKey loads and decrypts one keypair, including private key
// material. It is for local CLI/TUI use only; MCP must expose metadata instead.
func (m *Manager) LoadKeyPairWithKey(name string, cryptoKey []byte) (*KeyPairEntry, error) {
	var entry KeyPairEntry
	if err := m.loadSSHEntry(KeypairDirName, name, &entry, cryptoKey); err != nil {
		return nil, err
	}
	return &entry, nil
}

// LoadKeyPair derives the vault key from password and loads a keypair.
func (m *Manager) LoadKeyPair(name string, password string) (*KeyPairEntry, error) {
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return nil, err
	}
	return m.LoadKeyPairWithKey(name, cryptoKey)
}

// DeleteKeyPair removes one keypair. Reference protection belongs to the SSH
// manager, which can atomically clear host references in the same mutation.
func (m *Manager) DeleteKeyPair(name string) error {
	return m.deleteSSHEntry(KeypairDirName, name, "keypair")
}

// ListKeyPairs returns all valid keypair names, sorted by the trusted root.
func (m *Manager) ListKeyPairs() ([]string, error) {
	return m.listSSHEntries(KeypairDirName)
}

func (m *Manager) saveSSHEntry(dir, name string, entry any, cryptoKey []byte) error {
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.saveSSHEntry(dir, name, entry, cryptoKey)
		})
	}
	if err := m.requireCurrentKey(cryptoKey); err != nil {
		return err
	}
	data, err := ToJSON(entry)
	if err != nil {
		return fmt.Errorf("failed to serialize %s entry: %w", dir, err)
	}
	encrypted, err := crypto.Encrypt(cryptoKey, data)
	if err != nil {
		return fmt.Errorf("failed to encrypt SSH %s entry: %w", dir, err)
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.EnsureDir([]string{dir}, 0o700); err != nil {
		return fmt.Errorf("failed to create %s directory: %w", dir, err)
	}
	return root.AtomicWrite([]string{dir, name + ConfigFileSuffix}, []byte(encrypted), 0o600)
}

func (m *Manager) loadSSHEntry(dir, name string, target any, cryptoKey []byte) error {
	if err := validateEntryIdentity(entryKindForDir(dir), name); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.WithVaultMutation(func(locked *Manager) error {
			return locked.loadSSHEntry(dir, name, target, cryptoKey)
		})
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	encrypted, err := root.Read(dir, name+ConfigFileSuffix)
	if err != nil {
		return fmt.Errorf("%s %q not found: %w", entryKindForDir(dir), name, err)
	}
	decrypted, err := crypto.Decrypt(cryptoKey, string(encrypted))
	if err != nil {
		return fmt.Errorf("failed to decrypt %s %q: %w", entryKindForDir(dir), name, err)
	}
	if err := FromJSON(decrypted, target); err != nil {
		return fmt.Errorf("failed to parse %s %q: %w", entryKindForDir(dir), name, err)
	}
	return nil
}

func (m *Manager) deleteSSHEntry(dir, name, kind string) error {
	if err := validateSSHIdentity(kind, name); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.deleteSSHEntry(dir, name, kind) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := removeManagedFile(root, dir, name+ConfigFileSuffix); err != nil {
		return fmt.Errorf("failed to delete SSH %s %q: %w", kind, name, err)
	}
	return nil
}

func (m *Manager) listSSHEntries(dir string) ([]string, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) ([]string, error) {
			return locked.listSSHEntries(dir)
		})
	}
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := root.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to list %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir {
			return nil, fmt.Errorf("invalid managed %s directory %q", dir, entry.Name)
		}
		name := strings.TrimSuffix(entry.Name, ConfigFileSuffix)
		if !strings.HasSuffix(entry.Name, ConfigFileSuffix) {
			return nil, fmt.Errorf("invalid managed %s entry %q", dir, entry.Name)
		}
		if err := validateEntryIdentity(entryKindForDir(dir), name); err != nil {
			return nil, fmt.Errorf("invalid managed %s identity: %w", dir, err)
		}
		names = append(names, name)
	}
	return names, nil
}

func entryKindForDir(dir string) string {
	switch dir {
	case KeypairDirName:
		return "SSH keypair"
	case LLMProviderDirName:
		return "LLM provider"
	case MCPServerDirName:
		return "MCP server"
	default:
		return "SSH host"
	}
}
