package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wii/senv/internal/crypto"
)

// RekeyResult reports how many files were re-encrypted.
type RekeyResult struct {
	EnvFiles    int
	TextFiles   int
	ConfigFiles int
}

func (r *RekeyResult) Total() int {
	return r.EnvFiles + r.TextFiles + r.ConfigFiles
}

type rekeyEntry struct {
	path      string
	plaintext []byte
}

// Rekey re-encrypts all data files from oldKey to newKey and updates metadata
// with newSaltB64, newPasswordKey and the new KDF iteration count. On failure
// it attempts to restore the original encryption.
func (m *Manager) Rekey(oldKey, newKey []byte, newSaltB64, newPasswordKey string, newIterations int) (*RekeyResult, error) {
	entries, result, err := m.collectAndDecrypt(oldKey)
	if err != nil {
		return nil, fmt.Errorf("pre-flight decryption failed: %w", err)
	}

	if err := m.rekeyEncrypt(entries, newKey); err != nil {
		m.rekeyRollback(entries, oldKey)
		return nil, fmt.Errorf("re-encryption failed: %w", err)
	}

	if err := m.rekeyCommit(entries); err != nil {
		m.rekeyRollback(entries, oldKey)
		return nil, fmt.Errorf("commit failed: %w", err)
	}

	md, err := m.LoadMetadata()
	if err != nil {
		return nil, err
	}
	md.Salt = newSaltB64
	md.PasswordKey = newPasswordKey
	md.KDFIterations = newIterations
	md.UpdatedAt = time.Now()
	if err := m.SaveMetadata(md); err != nil {
		return nil, fmt.Errorf("failed to update metadata: %w", err)
	}

	return result, nil
}

func (m *Manager) collectAndDecrypt(oldKey []byte) ([]rekeyEntry, *RekeyResult, error) {
	var entries []rekeyEntry
	result := &RekeyResult{}

	envGroups, err := m.ListEnvGroups()
	if err != nil {
		return nil, nil, fmt.Errorf("list env groups: %w", err)
	}
	for _, group := range envGroups {
		// Old format: env_<group>.json.enc
		oldPath := filepath.Join(m.dataPath, fmt.Sprintf("%s%s%s", EnvFilePrefix, group, EnvFileSuffix))
		if _, statErr := os.Stat(oldPath); statErr == nil {
			plaintext, err := m.decryptFile(oldPath, oldKey)
			if err != nil {
				return nil, nil, fmt.Errorf("env group %q: %w", group, err)
			}
			entries = append(entries, rekeyEntry{path: oldPath, plaintext: plaintext})
			result.EnvFiles++
		}

		// New format: envs/<group>/*.enc (recursive for keys with path separators)
		groupDir := filepath.Join(m.dataPath, EnvDirName, group)
		walkErr := filepath.WalkDir(groupDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !isEncFile(d.Name()) {
				return nil
			}
			plaintext, decErr := m.decryptFile(path, oldKey)
			if decErr != nil {
				return fmt.Errorf("env var %s: %w", path, decErr)
			}
			entries = append(entries, rekeyEntry{path: path, plaintext: plaintext})
			result.EnvFiles++
			return nil
		})
		if walkErr != nil {
			return nil, nil, walkErr
		}
	}

	textGroups, err := m.ListTextGroups()
	if err != nil {
		return nil, nil, fmt.Errorf("list text groups: %w", err)
	}
	for _, group := range textGroups {
		keys, err := m.ListTextFiles(group)
		if err != nil {
			return nil, nil, fmt.Errorf("list text group %q: %w", group, err)
		}
		for _, k := range keys {
			path := m.textFilePath(group, k)
			plaintext, err := m.decryptFile(path, oldKey)
			if err != nil {
				return nil, nil, fmt.Errorf("text %s/%s: %w", group, k, err)
			}
			entries = append(entries, rekeyEntry{path: path, plaintext: plaintext})
			result.TextFiles++
		}
	}

	idx, err := m.LoadConfigIndex()
	if err == nil && idx != nil {
		for name, cf := range idx.Configs {
			fileName := cf.EncryptedFile
			if fileName == "" {
				fileName = name + ConfigFileSuffix
			}
			path := filepath.Join(m.dataPath, fileName)
			plaintext, err := m.decryptFile(path, oldKey)
			if err != nil {
				return nil, nil, fmt.Errorf("config %q: %w", name, err)
			}
			entries = append(entries, rekeyEntry{path: path, plaintext: plaintext})
			result.ConfigFiles++
		}
	}

	return entries, result, nil
}

func (m *Manager) decryptFile(path string, key []byte) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return crypto.Decrypt(key, string(data))
}

func (m *Manager) rekeyEncrypt(entries []rekeyEntry, newKey []byte) error {
	for _, e := range entries {
		encrypted, err := crypto.Encrypt(newKey, e.plaintext)
		if err != nil {
			return fmt.Errorf("encrypt %s: %w", e.path, err)
		}
		tmpPath := e.path + ".rekey-tmp"
		if err := WriteSensitiveFile(tmpPath, []byte(encrypted), 0o700, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", tmpPath, err)
		}
	}
	return nil
}

func (m *Manager) rekeyCommit(entries []rekeyEntry) error {
	for _, e := range entries {
		tmpPath := e.path + ".rekey-tmp"
		if err := os.Rename(tmpPath, e.path); err != nil {
			return fmt.Errorf("rename %s: %w", tmpPath, err)
		}
	}
	return nil
}

func (m *Manager) rekeyRollback(entries []rekeyEntry, oldKey []byte) {
	for _, e := range entries {
		tmpPath := e.path + ".rekey-tmp"
		if _, err := os.Stat(tmpPath); err == nil {
			os.Remove(tmpPath)
		}
	}
}

func isEncFile(name string) bool {
	return len(name) > 4 && name[len(name)-4:] == ".enc"
}
