package storage

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/securefs"
	"github.com/wii/senv/internal/syncschema"
)

// This file holds the rename/delete primitives used by the CLI and TUI editing
// surfaces. Under v2 AAD-bound envelopes, renaming an entry or group changes its
// authenticated identity and therefore decrypts and re-encrypts with the new
// AAD instead of performing a raw filesystem rename.

// renameEntry moves src -> dst below the trusted root for legacy v1 entries.
// Any v2 AAD-bound ciphertext is rejected with ErrEncryptedRenameRequiresKey.
func renameEntry(root securefs.TrustedRoot, src, dst []string, sourceIsDir bool) error {
	if sourceIsDir {
		entries, err := root.ReadDir(src...)
		if err != nil {
			return fmt.Errorf("rename source %s: %w", displaySegments(src), err)
		}
		for _, entry := range entries {
			if entry.IsDir {
				continue
			}
			child := append(append([]string(nil), src...), entry.Name)
			data, err := root.Read(child...)
			if err != nil {
				return fmt.Errorf("inspect rename source %s: %w", displaySegments(child), err)
			}
			if crypto.IsV2Ciphertext(string(data)) {
				return ErrEncryptedRenameRequiresKey
			}
		}
	} else {
		data, err := root.Read(src...)
		if err != nil {
			return fmt.Errorf("rename source %s: %w", displaySegments(src), err)
		}
		if crypto.IsV2Ciphertext(string(data)) {
			return ErrEncryptedRenameRequiresKey
		}
	}
	if err := requireAbsentTarget(root, dst, sourceIsDir); err != nil {
		return err
	}
	if err := root.Rename(src, dst); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", displaySegments(src), displaySegments(dst), err)
	}
	return nil
}

// requireAbsentTarget rejects a rename whose destination already exists, so
// renameat can never clobber a live entry.
func requireAbsentTarget(root securefs.TrustedRoot, dst []string, isDir bool) error {
	var err error
	if isDir {
		_, err = root.ReadDir(dst...)
	} else {
		_, err = root.Read(dst...)
	}
	switch {
	case err == nil:
		return fmt.Errorf("destination %s already exists", displaySegments(dst))
	case errors.Is(err, os.ErrNotExist):
		return nil
	default:
		return fmt.Errorf("inspect rename destination %s: %w", displaySegments(dst), err)
	}
}

func displaySegments(segments []string) string {
	return strings.Join(segments, "/")
}

func (m *Manager) renameSingleEntryWithKey(dirName, suffix, entryKind, group, oldKey, newKey string, cryptoKey []byte) error {
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.renameSingleEntryWithKey(dirName, suffix, entryKind, group, oldKey, newKey, cryptoKey)
		})
	}
	if err := m.requireCurrentKey(cryptoKey); err != nil {
		return err
	}
	policy, err := m.readCryptoPolicyLocked()
	if err != nil {
		return err
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()

	src := []string{dirName, group, oldKey + suffix}
	dst := []string{dirName, group, newKey + suffix}
	raw, err := root.Read(src...)
	if err != nil {
		return fmt.Errorf("rename source %s: %w", displaySegments(src), err)
	}
	if err := requireAbsentTarget(root, dst, false); err != nil {
		return err
	}
	plaintext, err := decryptWithPolicy(cryptoKey, entryKind, group, oldKey, string(raw), policy)
	if err != nil {
		return fmt.Errorf("decrypt %s for rename: %w", displaySegments(src), err)
	}
	newCT, err := encryptWithPolicy(cryptoKey, entryKind, group, newKey, plaintext, policy)
	if err != nil {
		return fmt.Errorf("encrypt %s for rename: %w", displaySegments(dst), err)
	}
	if err := root.AtomicWrite(dst, []byte(newCT), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", displaySegments(dst), err)
	}
	if err := root.Remove(src...); err != nil {
		_ = root.Remove(dst...)
		return fmt.Errorf("remove old entry %s: %w", displaySegments(src), err)
	}
	return nil
}

func (m *Manager) renameGroupWithKey(dirName, suffix, entryKind, metaKind, oldGroup, newGroup string, cryptoKey []byte) error {
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.renameGroupWithKey(dirName, suffix, entryKind, metaKind, oldGroup, newGroup, cryptoKey)
		})
	}
	if err := m.requireCurrentKey(cryptoKey); err != nil {
		return err
	}
	policy, err := m.readCryptoPolicyLocked()
	if err != nil {
		return err
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()

	entries, err := root.ReadDir(dirName, oldGroup)
	if err != nil {
		return fmt.Errorf("rename source %s/%s: %w", dirName, oldGroup, err)
	}
	if err := requireAbsentTarget(root, []string{dirName, newGroup}, true); err != nil {
		return err
	}

	type reencryptedFile struct {
		name    string
		content []byte
	}
	prepared := make([]reencryptedFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir {
			return fmt.Errorf("unexpected subdirectory %s/%s/%s during rename", dirName, oldGroup, entry.Name)
		}
		raw, err := root.Read(dirName, oldGroup, entry.Name)
		if err != nil {
			return err
		}
		if entry.Name == EnvMetaFileName {
			plain, err := decryptWithPolicy(cryptoKey, metaKind, oldGroup, "", string(raw), policy)
			if err != nil {
				return fmt.Errorf("decrypt group metadata %s/%s: %w", dirName, oldGroup, err)
			}
			var meta EnvGroupMeta
			if err := FromJSON(plain, &meta); err != nil {
				return err
			}
			meta.Name = newGroup
			updatedMeta, err := ToJSON(&meta)
			if err != nil {
				return err
			}
			newCT, err := encryptWithPolicy(cryptoKey, metaKind, newGroup, "", updatedMeta, policy)
			if err != nil {
				return err
			}
			prepared = append(prepared, reencryptedFile{name: entry.Name, content: []byte(newCT)})
			continue
		}
		if !strings.HasSuffix(entry.Name, suffix) {
			return fmt.Errorf("unexpected entry %s/%s/%s during rename", dirName, oldGroup, entry.Name)
		}
		key := strings.TrimSuffix(entry.Name, suffix)
		plain, err := decryptWithPolicy(cryptoKey, entryKind, oldGroup, key, string(raw), policy)
		if err != nil {
			return fmt.Errorf("decrypt %s/%s/%s: %w", dirName, oldGroup, entry.Name, err)
		}
		newCT, err := encryptWithPolicy(cryptoKey, entryKind, newGroup, key, plain, policy)
		if err != nil {
			return err
		}
		prepared = append(prepared, reencryptedFile{name: entry.Name, content: []byte(newCT)})
	}

	if err := root.EnsureDir([]string{dirName, newGroup}, 0o700); err != nil {
		return err
	}
	for _, f := range prepared {
		if err := root.AtomicWrite([]string{dirName, newGroup, f.name}, f.content, 0o600); err != nil {
			_ = root.RemoveTree(dirName, newGroup)
			return fmt.Errorf("write %s/%s/%s: %w", dirName, newGroup, f.name, err)
		}
	}
	if err := root.RemoveTree(dirName, oldGroup); err != nil {
		return fmt.Errorf("remove old group %s/%s: %w", dirName, oldGroup, err)
	}
	return nil
}

// RenameEnvVar atomically renames one legacy v1 env variable ciphertext inside its group.
// Rejects v2 ciphertexts; use RenameEnvVarWithKey for v2 vaults.
func (m *Manager) RenameEnvVar(group, oldKey, newKey string) error {
	if err := validateEnvIdentity(group, oldKey); err != nil {
		return err
	}
	if err := validateEnvIdentity(group, newKey); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.RenameEnvVar(group, oldKey, newKey) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return renameEntry(root,
		[]string{EnvDirName, group, oldKey + EnvVarSuffix},
		[]string{EnvDirName, group, newKey + EnvVarSuffix},
		false)
}

// RenameEnvVarWithKey renames one env variable and re-encrypts it under its new AAD.
func (m *Manager) RenameEnvVarWithKey(group, oldKey, newKey string, cryptoKey []byte) error {
	if err := validateEnvIdentity(group, oldKey); err != nil {
		return err
	}
	if err := validateEnvIdentity(group, newKey); err != nil {
		return err
	}
	return m.renameSingleEntryWithKey(EnvDirName, EnvVarSuffix, syncschema.KindEnv, group, oldKey, newKey, cryptoKey)
}

// RenameEnvGroupDir atomically renames a legacy v1 env group directory.
func (m *Manager) RenameEnvGroupDir(oldGroup, newGroup string) error {
	if err := ValidateName(oldGroup); err != nil {
		return fmt.Errorf("invalid env group %q: %w", oldGroup, err)
	}
	if err := ValidateName(newGroup); err != nil {
		return fmt.Errorf("invalid env group %q: %w", newGroup, err)
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.RenameEnvGroupDir(oldGroup, newGroup) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return renameEntry(root, []string{EnvDirName, oldGroup}, []string{EnvDirName, newGroup}, true)
}

// RenameEnvGroupWithKey renames an env group and re-encrypts its metadata and all entries under the new group AAD.
func (m *Manager) RenameEnvGroupWithKey(oldGroup, newGroup string, cryptoKey []byte) error {
	if err := ValidateName(oldGroup); err != nil {
		return fmt.Errorf("invalid env group %q: %w", oldGroup, err)
	}
	if err := ValidateName(newGroup); err != nil {
		return fmt.Errorf("invalid env group %q: %w", newGroup, err)
	}
	return m.renameGroupWithKey(EnvDirName, EnvVarSuffix, syncschema.KindEnv, syncschema.KindEnvMeta, oldGroup, newGroup, cryptoKey)
}

// DeleteEnvGroup removes the group directory (and any legacy single-blob file)
// after a complete no-follow preflight.
func (m *Manager) DeleteEnvGroup(group string) error {
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid env group %q: %w", group, err)
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.DeleteEnvGroup(group) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.RemoveTree(EnvDirName, group); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to delete env group %q: %w", group, err)
	}
	return removeManagedFile(root, EnvFilePrefix+group+EnvFileSuffix)
}

// RenameTextFile atomically renames one legacy v1 text block ciphertext inside its group.
func (m *Manager) RenameTextFile(group, oldKey, newKey string) error {
	if err := validateTextIdentity(group, oldKey); err != nil {
		return err
	}
	if err := validateTextIdentity(group, newKey); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.RenameTextFile(group, oldKey, newKey) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return renameEntry(root,
		[]string{TextDirName, group, oldKey + TextFileSuffix},
		[]string{TextDirName, group, newKey + TextFileSuffix},
		false)
}

// RenameTextFileWithKey renames one text block and re-encrypts it under its new AAD.
func (m *Manager) RenameTextFileWithKey(group, oldKey, newKey string, cryptoKey []byte) error {
	if err := validateTextIdentity(group, oldKey); err != nil {
		return err
	}
	if err := validateTextIdentity(group, newKey); err != nil {
		return err
	}
	return m.renameSingleEntryWithKey(TextDirName, TextFileSuffix, syncschema.KindText, group, oldKey, newKey, cryptoKey)
}

// RenameTextGroup atomically renames a legacy v1 text group directory.
func (m *Manager) RenameTextGroup(oldGroup, newGroup string) error {
	if err := ValidateName(oldGroup); err != nil {
		return fmt.Errorf("invalid text group %q: %w", oldGroup, err)
	}
	if err := ValidateName(newGroup); err != nil {
		return fmt.Errorf("invalid text group %q: %w", newGroup, err)
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.RenameTextGroup(oldGroup, newGroup) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return renameEntry(root, []string{TextDirName, oldGroup}, []string{TextDirName, newGroup}, true)
}

// RenameTextGroupWithKey renames a text group and re-encrypts its metadata and all blocks under the new group AAD.
func (m *Manager) RenameTextGroupWithKey(oldGroup, newGroup string, cryptoKey []byte) error {
	if err := ValidateName(oldGroup); err != nil {
		return fmt.Errorf("invalid text group %q: %w", oldGroup, err)
	}
	if err := ValidateName(newGroup); err != nil {
		return fmt.Errorf("invalid text group %q: %w", newGroup, err)
	}
	return m.renameGroupWithKey(TextDirName, TextFileSuffix, syncschema.KindText, syncschema.KindTextMeta, oldGroup, newGroup, cryptoKey)
}

func (m *Manager) RenameBackupFile(group, oldKey, newKey string) error {
	if err := validateBackupIdentity(group, oldKey); err != nil {
		return err
	}
	if err := validateBackupIdentity(group, newKey); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.RenameBackupFile(group, oldKey, newKey) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return renameEntry(root,
		[]string{BackupDirName, group, oldKey + BackupFileSuffix},
		[]string{BackupDirName, group, newKey + BackupFileSuffix},
		false)
}

// RenameBackupFileWithKey renames one backup block and re-encrypts it under its new AAD.
func (m *Manager) RenameBackupFileWithKey(group, oldKey, newKey string, cryptoKey []byte) error {
	if err := validateBackupIdentity(group, oldKey); err != nil {
		return err
	}
	if err := validateBackupIdentity(group, newKey); err != nil {
		return err
	}
	return m.renameSingleEntryWithKey(BackupDirName, BackupFileSuffix, syncschema.KindBackup, group, oldKey, newKey, cryptoKey)
}

func (m *Manager) RenameBackupGroup(oldGroup, newGroup string) error {
	if err := ValidateName(oldGroup); err != nil {
		return fmt.Errorf("invalid backup group %q: %w", oldGroup, err)
	}
	if err := ValidateName(newGroup); err != nil {
		return fmt.Errorf("invalid backup group %q: %w", newGroup, err)
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.RenameBackupGroup(oldGroup, newGroup) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return renameEntry(root, []string{BackupDirName, oldGroup}, []string{BackupDirName, newGroup}, true)
}

// RenameBackupGroupWithKey renames a backup group and re-encrypts its metadata and all blocks under the new group AAD.
func (m *Manager) RenameBackupGroupWithKey(oldGroup, newGroup string, cryptoKey []byte) error {
	if err := ValidateName(oldGroup); err != nil {
		return fmt.Errorf("invalid backup group %q: %w", oldGroup, err)
	}
	if err := ValidateName(newGroup); err != nil {
		return fmt.Errorf("invalid backup group %q: %w", newGroup, err)
	}
	return m.renameGroupWithKey(BackupDirName, BackupFileSuffix, syncschema.KindBackup, syncschema.KindBackupMeta, oldGroup, newGroup, cryptoKey)
}

// RenameConfigFile renames one legacy v1 config ciphertext and repoints its plaintext index entry.
func (m *Manager) RenameConfigFile(oldName, newName string) error {
	if err := validateConfigName(oldName); err != nil {
		return err
	}
	if err := validateConfigName(newName); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.RenameConfigFile(oldName, newName) })
	}
	index, err := m.LoadConfigIndex()
	if err != nil {
		return err
	}
	cfg, exists := index.Configs[oldName]
	if !exists {
		return fmt.Errorf("config %s not found", oldName)
	}
	if _, exists := index.Configs[newName]; exists {
		return fmt.Errorf("config %s already exists", newName)
	}
	oldEncrypted := cfg.EncryptedFile
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := renameEntry(root,
		[]string{oldEncrypted},
		[]string{newName + ConfigFileSuffix},
		false); err != nil {
		return err
	}

	delete(index.Configs, oldName)
	cfg.Name = newName
	cfg.EncryptedFile = newName + ConfigFileSuffix
	cfg.UpdatedAt = time.Now()
	index.Configs[newName] = cfg
	if err := m.SaveConfigIndex(index); err != nil {
		_ = root.Rename([]string{cfg.EncryptedFile}, []string{oldEncrypted})
		return fmt.Errorf("save config index after rename: %w", err)
	}
	return nil
}

// RenameConfigFileWithKey renames one config entry, re-encrypting its ciphertext
// under the new name AAD and saving the updated encrypted config index.
func (m *Manager) RenameConfigFileWithKey(oldName, newName string, cryptoKey []byte) error {
	if err := validateConfigName(oldName); err != nil {
		return err
	}
	if err := validateConfigName(newName); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.RenameConfigFileWithKey(oldName, newName, cryptoKey)
		})
	}
	if err := m.requireCurrentKey(cryptoKey); err != nil {
		return err
	}
	policy, err := m.readCryptoPolicyLocked()
	if err != nil {
		return err
	}
	index, err := m.LoadConfigIndexWithKey(cryptoKey)
	if err != nil {
		return err
	}
	cfg, exists := index.Configs[oldName]
	if !exists {
		return fmt.Errorf("config %s not found", oldName)
	}
	if _, exists := index.Configs[newName]; exists {
		return fmt.Errorf("config %s already exists", newName)
	}

	oldEncrypted := cfg.EncryptedFile
	newEncrypted := newName + ConfigFileSuffix
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()

	raw, err := root.Read(oldEncrypted)
	if err != nil {
		return fmt.Errorf("rename source %s: %w", oldEncrypted, err)
	}
	if err := requireAbsentTarget(root, []string{newEncrypted}, false); err != nil {
		return err
	}
	plain, err := decryptWithPolicy(cryptoKey, syncschema.KindConfig, "", oldName, string(raw), policy)
	if err != nil {
		return fmt.Errorf("decrypt config %q for rename: %w", oldName, err)
	}
	newCT, err := encryptWithPolicy(cryptoKey, syncschema.KindConfig, "", newName, plain, policy)
	if err != nil {
		return fmt.Errorf("encrypt config %q for rename: %w", newName, err)
	}
	if err := root.AtomicWrite([]string{newEncrypted}, []byte(newCT), 0o600); err != nil {
		return fmt.Errorf("write config %q: %w", newEncrypted, err)
	}

	delete(index.Configs, oldName)
	cfg.Name = newName
	cfg.EncryptedFile = newEncrypted
	cfg.UpdatedAt = time.Now()
	index.Configs[newName] = cfg
	if err := m.SaveConfigIndexWithKey(index, cryptoKey); err != nil {
		_ = root.Remove(newEncrypted)
		return fmt.Errorf("save config index after rename: %w", err)
	}
	if err := root.Remove(oldEncrypted); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove old config %q: %w", oldEncrypted, err)
	}
	return nil
}
