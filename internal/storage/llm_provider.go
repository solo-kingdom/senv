package storage

import (
	"fmt"
	"net/url"
	"strings"
)

// LLMProviderDirName is the top-level encrypted collection of LLM provider
// profiles, mirroring the SSH asset layout.
const LLMProviderDirName = "llm_providers"

func validateLLMProviderIdentity(alias string) error {
	return validateEntryIdentity("LLM provider", alias)
}

// ValidateLLMProviderURL validates an OpenAI-compatible base URL. HTTP is
// accepted only when a caller has made that explicit (for example --allow-http);
// userinfo is always rejected because it both leaks credentials and encourages
// embedding secrets in profile metadata.
func ValidateLLMProviderURL(raw string, allowHTTP bool) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("base URL must be an absolute http(s) URL with a host")
	}
	if u.User != nil {
		return fmt.Errorf("base URL must not contain userinfo")
	}
	if u.Scheme == "http" && !allowHTTP {
		return fmt.Errorf("base URL must use HTTPS by default; pass --allow-http only for trusted local/special environments")
	}
	return nil
}

// ValidateLLMCredentialRef validates the profile-side credential reference.
func ValidateLLMCredentialRef(ref string) error {
	kind, rest, found := strings.Cut(ref, ":")
	if !found || (kind != "env" && kind != "text") {
		return fmt.Errorf("credential ref must use env:<group>/<key> or text:<group>/<key>")
	}
	group, key, found := strings.Cut(rest, "/")
	if !found || group == "" || key == "" {
		return fmt.Errorf("credential ref must use %s:<group>/<key>", kind)
	}
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid credential ref group: %w", err)
	}
	if err := ValidateName(key); err != nil {
		return fmt.Errorf("invalid credential ref key: %w", err)
	}
	return nil
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
	// Decryption alone is not authorization to hand a malformed profile to a
	// switch, TUI, or MCP caller. Re-check all fields after decryption.
	if err := entry.ValidateLLMProvider(); err != nil {
		return nil, fmt.Errorf("invalid provider %q: %w", alias, err)
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
