package session

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrNoSecureSessionStore reports that no platform-verified secure store is
// available. Every occurrence must carry an actionable remediation hint.
var ErrNoSecureSessionStore = errors.New("no secure session store available")

const (
	// keychainLegacyAccount is the pre-slot account holding one cache for all vaults.
	keychainLegacyAccount = "senv.v1"
	keychainTrustedBinary = "/usr/bin/security"
	// keychainClearAllLimit bounds the delete loop; many vaults are fine, an
	// unbounded loop is not.
	keychainClearAllLimit = 1000
)

// securityRunner is the seam used by tests; production shells out to the
// Apple-signed security binary so locally rebuilt senv binaries do not churn
// keychain ACL signatures.
type securityRunner func(args []string, stdin string) (string, error)

var execSecurity securityRunner = func(args []string, stdin string) (string, error) {
	command := exec.Command(keychainTrustedBinary, args...)
	command.Stdin = strings.NewReader(stdin)
	output, err := command.Output()
	return string(output), err
}

func keychainServiceName() string {
	return fmt.Sprintf("senv.session.%d", os.Getuid())
}

// keychainAccount is the per-vault account name; each vault slot gets its own
// generic-password item.
func keychainAccount(slot string) string {
	return keychainLegacyAccount + "." + slot
}

// keychainStore persists session caches as login-keychain generic passwords.
// The payload is base64(JSON) so the interactive command line needs no quoting
// and the derived key never appears in process argv.
type keychainStore struct {
	runner securityRunner
	slot   string
}

func (s keychainStore) run(args []string, stdin string) (string, error) {
	runner := s.runner
	if runner == nil {
		runner = execSecurity
	}
	output, err := runner(args, stdin)
	if err != nil && !isSecurityItemNotFound(err) {
		return "", keychainStoreError(err)
	}
	return output, err
}

func keychainStoreError(err error) error {
	detail := err.Error()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) != 0 {
		if message := strings.TrimSpace(string(exitErr.Stderr)); message != "" {
			detail = message
		}
	}
	return fmt.Errorf(
		"%w: macOS Keychain unavailable: %s; unlock the login keychain, or for headless macOS or CI rerun with --insecure-cache",
		ErrNoSecureSessionStore, detail,
	)
}

func isSecurityItemNotFound(err error) bool {
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 44 {
		return true
	}
	return strings.Contains(err.Error(), "could not be found")
}

func (s keychainStore) Save(slot string, cache *SessionCache) error {
	data, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	// security -i reads commands from stdin, keeping the secret off argv.
	command := fmt.Sprintf(
		"add-generic-password -U -s %s -a %s -w %s -T %s\n",
		keychainServiceName(), keychainAccount(slot), encoded, keychainTrustedBinary,
	)
	if _, err := s.run([]string{"-i"}, command); err != nil {
		return err
	}
	return nil
}

func (s keychainStore) Load(slot string) (*SessionCache, error) {
	return s.loadAccount(keychainAccount(slot))
}

func (s keychainStore) LoadLegacy() (*SessionCache, error) {
	return s.loadAccount(keychainLegacyAccount)
}

func (s keychainStore) loadAccount(account string) (*SessionCache, error) {
	output, err := s.run(
		[]string{"find-generic-password", "-s", keychainServiceName(), "-a", account, "-w"},
		"",
	)
	if isSecurityItemNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(output))
	if err != nil {
		return nil, fmt.Errorf("failed to decode keychain cache: %w", err)
	}
	var cache SessionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("%w: corrupt keychain session cache; run: senv session clear --all", ErrSessionUnverifiable)
	}
	return &cache, nil
}

func (s keychainStore) Clear(slot string) error {
	return s.clearAccount(keychainAccount(slot))
}

func (s keychainStore) ClearLegacy() error {
	return s.clearAccount(keychainLegacyAccount)
}

func (s keychainStore) clearAccount(account string) error {
	_, err := s.run([]string{"delete-generic-password", "-s", keychainServiceName(), "-a", account}, "")
	if isSecurityItemNotFound(err) {
		return nil
	}
	return err
}

// ClearAll deletes every senv item under this user's service. Iterating the
// service-scoped delete covers both per-vault accounts and the legacy account.
func (s keychainStore) ClearAll() error {
	for i := 0; i < keychainClearAllLimit; i++ {
		_, err := s.run([]string{"delete-generic-password", "-s", keychainServiceName()}, "")
		if isSecurityItemNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	return fmt.Errorf("failed to clear keychain session caches: exceeded %d items", keychainClearAllLimit)
}
