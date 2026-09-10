package storage

import (
	"fmt"
)

// LLMProviderDirName is the top-level encrypted collection of LLM provider
// profiles, mirroring the SSH asset layout.
const LLMProviderDirName = "llm_providers"

func validateLLMProviderIdentity(alias string) error {
	return validateEntryIdentity("LLM provider", alias)
}

// SaveLLMProviderWithKey validates and atomically writes an encrypted
// provider profile.
func (m *Manager) SaveLLMProviderWithKey(alias string, entry *LLMProviderEntry, cryptoKey []byte) error {
	if err := validateLLMProviderIdentity(alias); err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("provider entry is nil")
	}
	if entry.Alias != alias {
		return fmt.Errorf("provider alias %q does not match entry alias %q", alias, entry.Alias)
	}
	if err := entry.ValidateLLMProvider(); err != nil {
		return err
	}
	return m.saveSSHEntry(LLMProviderDirName, alias, entry, cryptoKey)
}

// SaveLLMProvider derives the vault key from password and saves a provider
// profile.
func (m *Manager) SaveLLMProvider(alias string, entry *LLMProviderEntry, password string) error {
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return err
	}
	return m.SaveLLMProviderWithKey(alias, entry, cryptoKey)
}

// LoadLLMProviderWithKey loads and decrypts one provider profile.
func (m *Manager) LoadLLMProviderWithKey(alias string, cryptoKey []byte) (*LLMProviderEntry, error) {
	var entry LLMProviderEntry
	if err := m.loadSSHEntry(LLMProviderDirName, alias, &entry, cryptoKey); err != nil {
		return nil, err
	}
	return &entry, nil
}

// LoadLLMProvider derives the vault key from password and loads a provider
// profile.
func (m *Manager) LoadLLMProvider(alias string, password string) (*LLMProviderEntry, error) {
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return nil, err
	}
	return m.LoadLLMProviderWithKey(alias, cryptoKey)
}

// DeleteLLMProvider removes one provider profile. A missing alias is not an
// error, matching the existing idempotent entry deletion behavior.
func (m *Manager) DeleteLLMProvider(alias string) error {
	return m.deleteSSHEntry(LLMProviderDirName, alias, "LLM provider")
}

// ListLLMProviders returns all valid provider aliases, sorted by the trusted
// root.
func (m *Manager) ListLLMProviders() ([]string, error) {
	return m.listSSHEntries(LLMProviderDirName)
}
