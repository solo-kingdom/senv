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
	keychainAccount       = "senv.v1"
	keychainTrustedBinary = "/usr/bin/security"
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

// keychainStore persists the session cache as a login-keychain generic
// password. The payload is base64(JSON) so the interactive command line needs
// no quoting and the derived key never appears in process argv.
type keychainStore struct {
	runner securityRunner
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
	return fmt.Errorf("%w: macOS Keychain unavailable: %v; for headless macOS or CI rerun with --insecure-cache", ErrNoSecureSessionStore, err)
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

func (s keychainStore) Save(cache *SessionCache) error {
	data, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	// security -i reads commands from stdin, keeping the secret off argv.
	command := fmt.Sprintf(
		"add-generic-password -U -s %s -a %s -w %s -T %s\n",
		keychainServiceName(), keychainAccount, encoded, keychainTrustedBinary,
	)
	if _, err := s.run([]string{"-i"}, command); err != nil {
		return err
	}
	return nil
}

func (s keychainStore) Load() (*SessionCache, error) {
	output, err := s.run(
		[]string{"find-generic-password", "-s", keychainServiceName(), "-a", keychainAccount, "-w"},
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
		return nil, fmt.Errorf("failed to unmarshal keychain cache: %w", err)
	}
	return &cache, nil
}

func (s keychainStore) Clear() error {
	_, err := s.run([]string{"delete-generic-password", "-s", keychainServiceName()}, "")
	if isSecurityItemNotFound(err) {
		return nil
	}
	return err
}
