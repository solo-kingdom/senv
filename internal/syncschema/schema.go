// Package syncschema defines the portable identity schema shared by sync
// clients and servers. It deliberately validates only identities; ciphertext
// and revision policy remain the responsibility of their respective layers.
package syncschema

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/wii/senv/internal/securefs"
)

const (
	KindEnv         = "env"
	KindEnvMeta     = "env_meta"
	KindText        = "text"
	KindTextMeta    = "text_meta"
	KindConfig      = "config"
	KindConfigIndex = "config_index"
	KindLLMProvider = "llm_provider"
	KindMCPServer   = "mcp_server"
	KindSSHHost     = "ssh_host"
	KindSSHKeypair  = "ssh_keypair"
	KindBackup      = "backup"
	KindBackupMeta  = "backup_meta"
	KindSkill       = "skill"
	KindSkillIndex  = "skill_index"
	KindLedger      = "ledger"
	KindLedgerLog   = "ledger_log"
	KindLedgerMeta  = "ledger_meta"
	KindTree        = "tree"
	KindTreeMeta    = "tree_meta"
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

const (
	MaxTreePathBytes = 512
	MaxTreePathDepth = 16
)

// ValidateRelativePath validates a portable POSIX relative file path (no leading/trailing slash,
// no empty/dot segments, no backslashes or colons, bounded depth and length).
func ValidateRelativePath(rel string) error {
	if rel == "" || len(rel) > MaxTreePathBytes {
		return fmt.Errorf("relative path length must be 1..%d bytes", MaxTreePathBytes)
	}
	if strings.HasPrefix(rel, "/") || strings.HasSuffix(rel, "/") {
		return fmt.Errorf("relative path must not start or end with '/'")
	}
	if strings.ContainsAny(rel, "\\:\x00") {
		return fmt.Errorf("relative path contains forbidden character")
	}
	segs := strings.Split(rel, "/")
	if len(segs) > MaxTreePathDepth {
		return fmt.Errorf("relative path depth exceeds %d", MaxTreePathDepth)
	}
	for _, seg := range segs {
		if err := securefs.ValidateSegment(seg); err != nil {
			return err
		}
	}
	return nil
}

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
	case KindText, KindBackup, KindSkill, KindLedger, KindLedgerLog:
		if grp == "" || key == "" {
			return invalid("kind requires grp and key")
		}
		if err := securefs.ValidateSegment(grp); err != nil {
			return invalid("invalid grp")
		}
		if err := securefs.ValidateSegment(key); err != nil {
			return invalid("invalid key")
		}
	case KindTree:
		if grp == "" || key == "" {
			return invalid("kind requires grp and key")
		}
		if err := securefs.ValidateSegment(grp); err != nil {
			return invalid("invalid grp")
		}
		if err := ValidateRelativePath(key); err != nil {
			return invalid("invalid path")
		}
	case KindEnvMeta, KindTextMeta, KindBackupMeta, KindLedgerMeta, KindTreeMeta:
		if grp == "" || key != "" {
			return invalid("group meta requires grp and empty key")
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
	case KindSkillIndex:
		if grp != "" || key != "" {
			return invalid("skill_index requires empty grp and key")
		}
	case KindLLMProvider, KindMCPServer, KindSSHHost, KindSSHKeypair:
		if grp != "" || key == "" {
			return invalid("kind requires empty grp and key (alias)")
		}
		if err := securefs.ValidateSegment(key); err != nil {
			return invalid("invalid key")
		}
	default:
		return invalid("unknown kind")
	}
	return nil
}

func invalid(reason string) *ValidationError {
	return &ValidationError{reason: reason}
}
