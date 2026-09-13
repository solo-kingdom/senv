package ssh

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/storage"
)

// ErrExists reports an attempt to create an identity that is already present.
var ErrExists = errors.New("already exists")

// KeyPairSummary is safe metadata for listing, TUI, and MCP surfaces. It never
// contains PrivateKey.
type KeyPairSummary struct {
	Name        string    `json:"name"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	PublicKey   string    `json:"public_key,omitempty"`
	Comment     string    `json:"comment,omitempty"`
	Group       string    `json:"group,omitempty"`
	ImportedAt  time.Time `json:"imported_at"`
	HasPubKey   bool      `json:"has_pubkey"`
}

// Manager provides SSH asset operations above the encrypted storage layer.
type Manager struct {
	storage        *storage.Manager
	key            []byte
	password       string
	mutationLocked bool
}

func NewManager(store *storage.Manager, password string) *Manager {
	return &Manager{storage: store, password: password}
}

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

func validateExtra(extra map[string]string) error {
	for key, value := range extra {
		if key == "" || strings.ContainsAny(key, " \t\r\n\f\v") {
			return fmt.Errorf("invalid SSH attribute key %q: key must be non-empty and contain no whitespace", key)
		}
		if strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("invalid SSH attribute %q: value must not contain line separators or NUL", key)
		}
	}
	return nil
}

func validatePort(port int) error {
	if port < 0 || port > 65535 {
		return fmt.Errorf("invalid SSH port %d: must be between 0 and 65535", port)
	}
	return nil
}

func validateTags(tags []string) error {
	for _, tag := range tags {
		if strings.TrimSpace(tag) == "" {
			return fmt.Errorf("invalid SSH tag %q: must not be blank", tag)
		}
	}
	return nil
}

// validateGroup rejects group values that could break the single-line JSON
// record format. Empty (ungrouped) is valid.
func validateGroup(group string) error {
	if strings.ContainsAny(group, "\r\n\x00") {
		return fmt.Errorf("group must not contain line separators or NUL")
	}
	return nil
}

// ImportKeyPair reads and validates a private key file before encrypting it.
// A passphrase-protected key still imports, but no public key can be derived.
func (m *Manager) ImportKeyPair(name, path string, force bool) (*KeyPairSummary, error) {
	return m.ImportKeyPairWithGroup(name, path, "", force)
}

// ImportKeyPairWithGroup is ImportKeyPair plus the initial group membership.
func (m *Manager) ImportKeyPairWithGroup(name, path, group string, force bool) (*KeyPairSummary, error) {
	if err := storage.ValidateName(name); err != nil {
		return nil, fmt.Errorf("invalid keypair name %q: %w", name, err)
	}
	if err := validateGroup(group); err != nil {
		return nil, fmt.Errorf("keypair %q: %w", name, err)
	}
	privateKey, err := os.ReadFile(path) //nolint:gosec // the user explicitly supplies this path
	if err != nil {
		return nil, fmt.Errorf("read private key %q: %w", path, err)
	}
	publicKey, fingerprint, comment, parseErr := derivePublicKey(privateKey)
	if parseErr != nil {
		return nil, fmt.Errorf("validate private key %q: %w", path, parseErr)
	}
	now := time.Now().UTC()
	entry := &storage.KeyPairEntry{
		Name:        name,
		PrivateKey:  string(privateKey),
		PublicKey:   publicKey,
		Fingerprint: fingerprint,
		Comment:     comment,
		Group:       group,
		ImportedAt:  now,
	}
	var summary *KeyPairSummary
	err = m.mutate(func(locked *Manager) error {
		_, loadErr := locked.loadKeyPair(name)
		switch {
		case loadErr == nil && !force:
			return fmt.Errorf("keypair %q %w (use --force to overwrite)", name, ErrExists)
		case loadErr != nil && !errors.Is(loadErr, os.ErrNotExist):
			return loadErr
		}
		if err := locked.saveKeyPair(entry); err != nil {
			return err
		}
		value := keyPairSummary(entry)
		summary = &value
		return nil
	})
	if err != nil {
		return nil, err
	}
	return summary, nil
}

// derivePublicKey best-effort derives the authorized-key form and fingerprint.
// Errors are deliberately collapsed into empty values because encrypted keys
// are valid imports even though senv never asks for their passphrase.
func derivePublicKey(privateKey []byte) (publicKey, fingerprint, comment string, err error) {
	raw, err := ssh.ParseRawPrivateKey(privateKey)
	if err != nil {
		var passphraseErr *ssh.PassphraseMissingError
		if errors.As(err, &passphraseErr) {
			return "", "", "", nil
		}
		return "", "", "", err
	}
	signer, err := ssh.NewSignerFromKey(raw)
	if err != nil {
		return "", "", "", err
	}
	publicKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	fingerprint = ssh.FingerprintSHA256(signer.PublicKey())
	return publicKey, fingerprint, signer.PublicKey().Type(), nil
}

// ListKeyPairs returns safe keypair metadata sorted by name.
// ListKeyPairs 列出全部密钥对摘要，附耗时日志。
func (m *Manager) ListKeyPairs() ([]KeyPairSummary, error) {
	st := perflog.Start("ssh.list-keypairs")
	res, err := m.listKeyPairsSummaries()
	st.EndErr(err)
	return res, err
}

func (m *Manager) listKeyPairsSummaries() ([]KeyPairSummary, error) {
	names, err := m.listKeyPairs()
	if err != nil {
		return nil, err
	}
	summaries := make([]KeyPairSummary, 0, len(names))
	for _, name := range names {
		entry, err := m.loadKeyPair(name)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, keyPairSummary(entry))
	}
	return summaries, nil
}

// GetKeyPairSummary returns keypair metadata without private key material.
func (m *Manager) GetKeyPairSummary(name string) (*KeyPairSummary, error) {
	entry, err := m.loadKeyPair(name)
	if err != nil {
		return nil, err
	}
	summary := keyPairSummary(entry)
	return &summary, nil
}

// UpdateKeyPair applies a callback to an existing keypair record and saves the
// result after validating it, mirroring UpdateHost. Name must not change; key
// material is edited at the caller's own risk.
func (m *Manager) UpdateKeyPair(name string, update func(*storage.KeyPairEntry) error) error {
	if err := storage.ValidateName(name); err != nil {
		return fmt.Errorf("invalid keypair name %q: %w", name, err)
	}
	return m.mutate(func(locked *Manager) error {
		entry, err := locked.loadKeyPair(name)
		if err != nil {
			return err
		}
		if update != nil {
			if err := update(entry); err != nil {
				return err
			}
		}
		if entry.Name != name {
			return fmt.Errorf("keypair name cannot be renamed from %q to %q", name, entry.Name)
		}
		if err := validateGroup(entry.Group); err != nil {
			return fmt.Errorf("keypair %q: %w", name, err)
		}
		return locked.saveKeyPair(entry)
	})
}

// RenameKeyPair renames a keypair and rewrites every host reference in the
// same vault mutation, so a host can never be left pointing at a keypair that
// does not exist. Key material and metadata are stored verbatim under the new
// name. The returned aliases are the hosts whose identityKey was updated.
func (m *Manager) RenameKeyPair(oldName, newName string) ([]string, error) {
	if err := storage.ValidateName(oldName); err != nil {
		return nil, fmt.Errorf("invalid keypair name %q: %w", oldName, err)
	}
	if err := storage.ValidateName(newName); err != nil {
		return nil, fmt.Errorf("invalid keypair name %q: %w", newName, err)
	}
	if oldName == newName {
		return nil, nil
	}
	var updated []string
	err := m.mutate(func(locked *Manager) error {
		updated = nil
		entry, err := locked.loadKeyPair(oldName)
		if err != nil {
			return err
		}
		if _, err := locked.loadKeyPair(newName); err == nil {
			return fmt.Errorf("keypair %q %w", newName, ErrExists)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}

		renamed := *entry
		renamed.Name = newName
		if err := locked.saveKeyPair(&renamed); err != nil {
			return fmt.Errorf("write renamed keypair %q: %w", newName, err)
		}

		// Rewrite references before dropping the old entry: a failure at any
		// point still leaves every host pointing at an existing keypair.
		hosts, err := locked.storage.ListHosts()
		if err != nil {
			return err
		}
		rollback := func(cause error) error {
			for _, alias := range updated {
				if host, err := locked.loadHost(alias); err == nil {
					host.IdentityKey = oldName
					host.Alias = alias
					_ = locked.saveHost(host)
				}
			}
			_ = locked.storage.DeleteKeyPair(newName)
			return cause
		}
		for _, alias := range hosts {
			host, err := locked.loadHost(alias)
			if err != nil {
				return rollback(err)
			}
			if host.IdentityKey != oldName {
				continue
			}
			host.IdentityKey = newName
			host.UpdatedAt = time.Now().UTC()
			host.Alias = alias
			if err := locked.saveHost(host); err != nil {
				return rollback(fmt.Errorf("update host %q: %w", alias, err))
			}
			updated = append(updated, alias)
		}
		if err := locked.storage.DeleteKeyPair(oldName); err != nil {
			return rollback(fmt.Errorf("remove old keypair %q: %w", oldName, err))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(updated)
	return updated, nil
}

// MaterializePath is the stable public convention documented by ADR-0001.
func MaterializePath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".ssh", "senv", name), nil
}

// Materialize decrypts one private key into ~/.ssh/senv/<name>. Existing
// files require force; the parent directory and file are created 0700/0600.
func (m *Manager) Materialize(name string, force bool) (string, error) {
	entry, err := m.loadKeyPair(name)
	if err != nil {
		return "", err
	}
	target, err := MaterializePath(name)
	if err != nil {
		return "", err
	}
	if !force {
		if _, statErr := os.Lstat(target); statErr == nil {
			return "", fmt.Errorf("materialized key %q already exists (use --force to overwrite)", target)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", fmt.Errorf("inspect materialized key %q: %w", target, statErr)
		}
	}
	if err := storage.EnsurePrivateDir(filepath.Dir(target), 0o700); err != nil {
		return "", err
	}
	if err := storage.WriteSensitiveFile(target, []byte(entry.PrivateKey), 0o700, 0o600); err != nil {
		return "", fmt.Errorf("materialize keypair %q: %w", name, err)
	}
	return target, nil
}

// DeleteKeyPair refuses to remove a key still referenced by a host. Force
// clears every reference and deletes the key in one vault mutation.
func (m *Manager) DeleteKeyPair(name string, force bool) ([]string, error) {
	var referencers []string
	err := m.mutate(func(locked *Manager) error {
		if _, err := locked.loadKeyPair(name); err != nil {
			return err
		}
		hosts, err := locked.storage.ListHosts()
		if err != nil {
			return err
		}
		referencers = nil
		for _, alias := range hosts {
			host, err := locked.loadHost(alias)
			if err != nil {
				return err
			}
			if host.IdentityKey == name {
				referencers = append(referencers, alias)
			}
		}
		if len(referencers) > 0 && !force {
			sort.Strings(referencers)
			return fmt.Errorf("keypair %q is referenced by host(s): %s", name, strings.Join(referencers, ", "))
		}
		for _, alias := range referencers {
			host, err := locked.loadHost(alias)
			if err != nil {
				return err
			}
			host.IdentityKey = ""
			host.UpdatedAt = time.Now().UTC()
			host.Alias = alias
			if err := locked.saveHost(host); err != nil {
				return err
			}
		}
		return locked.storage.DeleteKeyPair(name)
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(referencers)
	return referencers, nil
}

func (m *Manager) loadKeyPair(name string) (*storage.KeyPairEntry, error) {
	if m.key != nil {
		return m.storage.LoadKeyPairWithKey(name, m.key)
	}
	return m.storage.LoadKeyPair(name, m.password)
}

func (m *Manager) saveKeyPair(entry *storage.KeyPairEntry) error {
	if m.key != nil {
		return m.storage.SaveKeyPairWithKey(entry.Name, entry, m.key)
	}
	return m.storage.SaveKeyPair(entry.Name, entry, m.password)
}

func (m *Manager) listKeyPairs() ([]string, error) {
	if m.mutationLocked {
		return m.storage.ListKeyPairs()
	}
	var names []string
	err := m.storage.WithVaultMutation(func(locked *storage.Manager) error {
		var err error
		names, err = locked.ListKeyPairs()
		return err
	})
	return names, err
}

func keyPairSummary(entry *storage.KeyPairEntry) KeyPairSummary {
	return KeyPairSummary{
		Name:        entry.Name,
		Fingerprint: entry.Fingerprint,
		PublicKey:   entry.PublicKey,
		Comment:     entry.Comment,
		Group:       entry.Group,
		ImportedAt:  entry.ImportedAt,
		HasPubKey:   entry.PublicKey != "",
	}
}
