package storage

import (
	"errors"
	"os"
	"strings"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/syncschema"
)

const (
	metadataVerifierAADKind = "metadata_verifier"
	legacyEnvAADKind        = "env_legacy"
)

var (
	// ErrPlaintextIndexRejected is returned when require_v2 is active and an
	// index file is stored as plaintext JSON instead of a v2 encrypted envelope.
	ErrPlaintextIndexRejected = errors.New("plaintext index rejected by require_v2 policy")

	// ErrEncryptedRenameRequiresKey is returned when an unkeyed rename helper
	// encounters a v2 AAD-bound entry that must be re-encrypted under its new identity.
	ErrEncryptedRenameRequiresKey = errors.New("renaming a v2 AAD-bound entry requires the vault key")
)

type vaultCryptoPolicy struct {
	KeyEpoch  int
	RequireV2 bool
}

func metadataVerifierAAD(keyEpoch int) []byte {
	return crypto.BuildEntryAAD(metadataVerifierAADKind, "", "", keyEpoch)
}

func (m *Manager) readCryptoPolicyLocked() (vaultCryptoPolicy, error) {
	var policy vaultCryptoPolicy
	if _, err := os.Stat(m.configPath); errors.Is(err, os.ErrNotExist) {
		return policy, nil
	}
	root, err := m.openConfigRoot()
	if err != nil {
		return policy, err
	}
	defer root.Close()

	if data, err := root.Read(MetadataFile); err == nil {
		md, parseErr := ParseMetadata(data)
		if parseErr != nil {
			return policy, parseErr
		}
		policy.KeyEpoch = md.KeyEpoch
	} else if !errors.Is(err, os.ErrNotExist) {
		return policy, err
	}

	if data, err := root.Read(SettingsFile); err == nil {
		var settings Settings
		if parseErr := FromJSON(data, &settings); parseErr != nil {
			return policy, parseErr
		}
		policy.RequireV2 = settings.Vault.RequireV2
	} else if !errors.Is(err, os.ErrNotExist) {
		return policy, err
	}

	return policy, nil
}

func encryptWithPolicy(cryptoKey []byte, kind, grp, key string, plaintext []byte, policy vaultCryptoPolicy) (string, error) {
	aad := crypto.BuildEntryAAD(kind, grp, key, policy.KeyEpoch)
	return crypto.EncryptWithAAD(cryptoKey, plaintext, aad)
}

func decryptWithPolicy(cryptoKey []byte, kind, grp, key string, encoded string, policy vaultCryptoPolicy) ([]byte, error) {
	aad := crypto.BuildEntryAAD(kind, grp, key, policy.KeyEpoch)
	return crypto.DecryptWithAADStrict(cryptoKey, encoded, aad, policy.RequireV2)
}

func decryptWithAEAD(aead *crypto.AEAD, kind, grp, key string, encoded string, policy vaultCryptoPolicy) ([]byte, error) {
	aad := crypto.BuildEntryAAD(kind, grp, key, policy.KeyEpoch)
	return aead.DecryptWithAADStrict(encoded, aad, policy.RequireV2)
}

func (m *Manager) encryptEntryLocked(cryptoKey []byte, kind, grp, key string, plaintext []byte) (string, error) {
	policy, err := m.readCryptoPolicyLocked()
	if err != nil {
		return "", err
	}
	return encryptWithPolicy(cryptoKey, kind, grp, key, plaintext, policy)
}

func (m *Manager) decryptEntryLocked(cryptoKey []byte, kind, grp, key string, encoded string) ([]byte, error) {
	policy, err := m.readCryptoPolicyLocked()
	if err != nil {
		return nil, err
	}
	return decryptWithPolicy(cryptoKey, kind, grp, key, encoded, policy)
}

func sshAADKindForDir(dir string) string {
	switch dir {
	case KeypairDirName:
		return syncschema.KindSSHKeypair
	case LLMProviderDirName:
		return syncschema.KindLLMProvider
	case MCPServerDirName:
		return syncschema.KindMCPServer
	default:
		return syncschema.KindSSHHost
	}
}

// entryAADIdentity maps a relative data-root segment path to its canonical
// (kind, grp, key) AAD tuple matching internal/syncschema/schema.go.
func entryAADIdentity(segments []string) (kind, grp, key string, ok bool) {
	switch {
	case len(segments) == 1 && segments[0] == SkillIndexFile:
		return syncschema.KindSkillIndex, "", "", true
	case len(segments) == 1 && strings.HasPrefix(segments[0], EnvFilePrefix) && strings.HasSuffix(segments[0], EnvFileSuffix):
		group := strings.TrimSuffix(strings.TrimPrefix(segments[0], EnvFilePrefix), EnvFileSuffix)
		return legacyEnvAADKind, group, "", true
	case len(segments) == 1 && strings.HasSuffix(segments[0], ConfigFileSuffix):
		name := strings.TrimSuffix(segments[0], ConfigFileSuffix)
		return syncschema.KindConfig, "", name, true
	case len(segments) == 3 && segments[0] == EnvDirName:
		if segments[2] == EnvMetaFileName {
			return syncschema.KindEnvMeta, segments[1], "", true
		}
		if strings.HasSuffix(segments[2], EnvVarSuffix) {
			return syncschema.KindEnv, segments[1], strings.TrimSuffix(segments[2], EnvVarSuffix), true
		}
	case len(segments) == 3 && segments[0] == TextDirName:
		if segments[2] == EnvMetaFileName {
			return syncschema.KindTextMeta, segments[1], "", true
		}
		if strings.HasSuffix(segments[2], TextFileSuffix) {
			return syncschema.KindText, segments[1], strings.TrimSuffix(segments[2], TextFileSuffix), true
		}
	case len(segments) == 3 && segments[0] == BackupDirName:
		if segments[2] == EnvMetaFileName {
			return syncschema.KindBackupMeta, segments[1], "", true
		}
		if strings.HasSuffix(segments[2], BackupFileSuffix) {
			return syncschema.KindBackup, segments[1], strings.TrimSuffix(segments[2], BackupFileSuffix), true
		}
	case len(segments) == 3 && segments[0] == SkillDirName:
		if strings.HasSuffix(segments[2], SkillFileSuffix) {
			return syncschema.KindSkill, segments[1], strings.TrimSuffix(segments[2], SkillFileSuffix), true
		}
	case len(segments) == 3 && segments[0] == LedgerDirName:
		if segments[2] == EnvMetaFileName {
			return syncschema.KindLedgerMeta, segments[1], "", true
		}
		if strings.HasSuffix(segments[2], LedgerFileSuffix) {
			return syncschema.KindLedger, segments[1], strings.TrimSuffix(segments[2], LedgerFileSuffix), true
		}
	case len(segments) == 3 && segments[0] == LedgerLogDirName:
		if strings.HasSuffix(segments[2], LedgerFileSuffix) {
			return syncschema.KindLedgerLog, segments[1], strings.TrimSuffix(segments[2], LedgerFileSuffix), true
		}
	case len(segments) >= 3 && segments[0] == TreeDirName:
		if len(segments) == 3 && segments[2] == EnvMetaFileName {
			return syncschema.KindTreeMeta, segments[1], "", true
		}
		last := segments[len(segments)-1]
		if strings.HasSuffix(last, TreeFileSuffix) {
			relParts := append([]string(nil), segments[2:len(segments)-1]...)
			relParts = append(relParts, strings.TrimSuffix(last, TreeFileSuffix))
			return syncschema.KindTree, segments[1], strings.Join(relParts, "/"), true
		}
	case len(segments) == 2 && segments[0] == HostDirName:
		if strings.HasSuffix(segments[1], ConfigFileSuffix) {
			return syncschema.KindSSHHost, "", strings.TrimSuffix(segments[1], ConfigFileSuffix), true
		}
	case len(segments) == 2 && segments[0] == KeypairDirName:
		if strings.HasSuffix(segments[1], ConfigFileSuffix) {
			return syncschema.KindSSHKeypair, "", strings.TrimSuffix(segments[1], ConfigFileSuffix), true
		}
	case len(segments) == 2 && segments[0] == LLMProviderDirName:
		if strings.HasSuffix(segments[1], ConfigFileSuffix) {
			return syncschema.KindLLMProvider, "", strings.TrimSuffix(segments[1], ConfigFileSuffix), true
		}
	case len(segments) == 2 && segments[0] == MCPServerDirName:
		if strings.HasSuffix(segments[1], ConfigFileSuffix) {
			return syncschema.KindMCPServer, "", strings.TrimSuffix(segments[1], ConfigFileSuffix), true
		}
	}
	return "", "", "", false
}
