// Package syncschema defines the portable identity schema shared by sync
// clients and servers. It deliberately validates only identities; ciphertext
// and revision policy remain the responsibility of their respective layers.
package syncschema

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/wii/senv/internal/securefs"
)

const (
	KindEnv         = "env"
	KindEnvMeta     = "env_meta"
	KindText        = "text"
	KindConfig      = "config"
	KindConfigIndex = "config_index"
)

// ErrInvalidIdentity allows callers to classify malformed remote identities
// without exposing attacker-controlled identity bytes in user-facing errors.
var ErrInvalidIdentity = errors.New("invalid sync entry identity")

var validEnvKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateEnvKey applies the shell-variable rule required by synchronized env
// entries as well as local storage and export.
func ValidateEnvKey(key string) error {
	if !validEnvKeyRE.MatchString(key) {
		return fmt.Errorf("env key must match [A-Za-z_][A-Za-z0-9_]*")
	}
	return nil
}

// ValidationError describes a stable, sanitized identity validation failure.
type ValidationError struct {
	reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", ErrInvalidIdentity, e.reason)
}

func (e *ValidationError) Unwrap() error { return ErrInvalidIdentity }

// ValidateIdentity enforces the exact grp/key matrix for every supported kind.
func ValidateIdentity(kind, grp, key string) error {
	switch kind {
	case KindEnv:
		if grp == "" || key == "" {
			return invalid("kind requires grp and key")
		}
		if err := securefs.ValidateSegment(grp); err != nil {
			return invalid("invalid grp")
		}
		if err := securefs.ValidateSegment(key); err != nil {
			return invalid("invalid key")
		}
		if err := ValidateEnvKey(key); err != nil {
			return invalid("invalid env key")
		}
	case KindText:
		if grp == "" || key == "" {
			return invalid("kind requires grp and key")
		}
		if err := securefs.ValidateSegment(grp); err != nil {
			return invalid("invalid grp")
		}
		if err := securefs.ValidateSegment(key); err != nil {
			return invalid("invalid key")
		}
	case KindEnvMeta:
		if grp == "" || key != "" {
			return invalid("env_meta requires grp and empty key")
		}
		if err := securefs.ValidateSegment(grp); err != nil {
			return invalid("invalid grp")
		}
	case KindConfig:
		if grp != "" || key == "" {
			return invalid("config requires empty grp and key")
		}
		if err := securefs.ValidateSegment(key); err != nil {
			return invalid("invalid key")
		}
	case KindConfigIndex:
		if grp != "" || key != "" {
			return invalid("config_index requires empty grp and key")
		}
	default:
		return invalid("unknown kind")
	}
	return nil
}

func invalid(reason string) *ValidationError {
	return &ValidationError{reason: reason}
}
