package ssh

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/storage"
)

// AddHost creates a host and enforces all structured references before any
// encrypted record is written.
func (m *Manager) AddHost(host *storage.HostEntry) error {
	if host == nil {
		return fmt.Errorf("host entry is nil")
	}
	return m.mutate(func(locked *Manager) error {
		if _, err := locked.loadHost(host.Alias); err == nil {
			return fmt.Errorf("host %q %w", host.Alias, ErrExists)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		host.UpdatedAt = time.Now().UTC()
		return locked.validateAndSave(host)
	})
}

// GetHost decrypts one host record.
func (m *Manager) GetHost(alias string) (*storage.HostEntry, error) {
	return m.loadHost(alias)
}

// ListHosts decrypts all hosts sorted by alias.
// ListHosts 列出全部主机档案，附耗时日志。
func (m *Manager) ListHosts() ([]*storage.HostEntry, error) {
	st := perflog.Start("ssh.list-hosts")
	res, err := m.listHostsEntries()
	if err == nil {
		st.With("hosts", len(res))
	}
	st.EndErr(err)
	return res, err
}

func (m *Manager) listHostsEntries() ([]*storage.HostEntry, error) {
	var names []string
	err := m.storage.WithVaultMutation(func(locked *storage.Manager) error {
		var err error
		names, err = locked.ListHosts()
		return err
	})
	if err != nil {
		return nil, err
	}
	hosts := make([]*storage.HostEntry, 0, len(names))
	for _, alias := range names {
		host, err := m.loadHost(alias)
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, host)
	}
	return hosts, nil
}

// UpdateHost replaces a host after validating the resulting references. The
// callback receives the existing record and may edit it in place.
func (m *Manager) UpdateHost(alias string, update func(*storage.HostEntry) error) error {
	return m.mutate(func(locked *Manager) error {
		host, err := locked.loadHost(alias)
		if err != nil {
			return err
		}
		if update != nil {
			if err := update(host); err != nil {
				return err
			}
		}
		if host.Alias != alias {
			return fmt.Errorf("host alias cannot be renamed from %q to %q", alias, host.Alias)
		}
		host.UpdatedAt = time.Now().UTC()
		return locked.validateAndSave(host)
	})
}

// DeleteHost removes a host. Host aliases are not referenced from keypairs, so
// no reverse-reference scan is required.
func (m *Manager) DeleteHost(alias string) error {
	return m.mutate(func(locked *Manager) error {
		if _, err := locked.loadHost(alias); err != nil {
			return err
		}
		return locked.storage.DeleteHost(alias)
	})
}

func (m *Manager) validateAndSave(host *storage.HostEntry) error {
	if err := validateHost(host); err != nil {
		return err
	}
	if host.IdentityKey != "" {
		if _, err := m.loadKeyPair(host.IdentityKey); err != nil {
			return fmt.Errorf("validate identityKey %q: %w", host.IdentityKey, err)
		}
	}
	if host.ProxyJump != "" {
		if host.ProxyJump == host.Alias {
			return fmt.Errorf("host %q cannot be its own proxyJump", host.Alias)
		}
		if _, err := m.loadHost(host.ProxyJump); err != nil {
			return fmt.Errorf("validate proxyJump %q: %w", host.ProxyJump, err)
		}
	}
	// When the vault is mutation-locked, the wrapped storage manager still has
	// no crypto key in password mode; delegate key derivation to storage.
	if m.key != nil {
		return m.storage.SaveHostWithKey(host.Alias, host, m.key)
	}
	return m.storage.SaveHost(host.Alias, host, m.password)
}

func validateHost(host *storage.HostEntry) error {
	if host == nil {
		return fmt.Errorf("host entry is nil")
	}
	if err := storage.ValidateName(host.Alias); err != nil {
		return fmt.Errorf("invalid host alias %q: %w", host.Alias, err)
	}
	if strings.TrimSpace(host.Hostname) == "" {
		return fmt.Errorf("host %q: hostname is required", host.Alias)
	}
	if strings.ContainsAny(host.Hostname, "\r\n\x00") {
		return fmt.Errorf("host %q: hostname must not contain line separators or NUL", host.Alias)
	}
	if strings.ContainsAny(host.User, "\r\n\x00") {
		return fmt.Errorf("host %q: user must not contain line separators or NUL", host.Alias)
	}
	if err := validateGroup(host.Group); err != nil {
		return fmt.Errorf("host %q: %w", host.Alias, err)
	}
	if err := validatePort(host.Port); err != nil {
		return err
	}
	if err := validateTags(host.Tags); err != nil {
		return err
	}
	return validateExtra(host.Extra)
}

// renderHost writes one Host block; identityPath is the precomputed
// MaterializePath of the host's keypair ("" when the host has none).
func renderHost(b *strings.Builder, host *storage.HostEntry, identityPath string) {
	fmt.Fprintf(b, "Host %s\n", host.Alias)
	if host.Hostname != "" {
		fmt.Fprintf(b, "  HostName %s\n", host.Hostname)
	}
	if host.User != "" {
		fmt.Fprintf(b, "  User %s\n", host.User)
	}
	if host.Port != 0 {
		fmt.Fprintf(b, "  Port %d\n", host.Port)
	}
	if host.ProxyJump != "" {
		fmt.Fprintf(b, "  ProxyJump %s\n", host.ProxyJump)
	}
	if identityPath != "" {
		fmt.Fprintf(b, "  IdentityFile %s\n", identityPath)
	}
	keys := make([]string, 0, len(host.Extra))
	for key := range host.Extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(b, "  %s %s\n", key, host.Extra[key])
	}
}

func (m *Manager) loadHost(alias string) (*storage.HostEntry, error) {
	if m.key != nil {
		return m.storage.LoadHostWithKey(alias, m.key)
	}
	return m.storage.LoadHost(alias, m.password)
}

func (m *Manager) saveHost(host *storage.HostEntry) error {
	if m.key != nil {
		return m.storage.SaveHostWithKey(host.Alias, host, m.key)
	}
	return m.storage.SaveHost(host.Alias, host, m.password)
}
