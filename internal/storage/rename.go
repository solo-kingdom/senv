package storage

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wii/senv/internal/securefs"
)

// This file holds the rename/delete primitives used by the TUI editing surface.
//
// Design: every rename is a single atomic filesystem operation (renameat below
// one trusted root) instead of "write new + delete old", so a crash can never
// leave two live copies or lose the ciphertext. Payloads that carry no identity
// (env vars, text blocks, config ciphertext) can be moved verbatim; identity
// that lives inside the payload (env group metadata, the config index) is
// rewritten right after the move, with a compensating rollback when that write
// fails.

// renameEntry moves src -> dst below the trusted root. The source must exist
// and the destination must not, so a rename never overwrites data.
func renameEntry(root securefs.TrustedRoot, src, dst []string, sourceIsDir bool) error {
	if sourceIsDir {
		if _, err := root.ReadDir(src...); err != nil {
			return fmt.Errorf("rename source %s: %w", displaySegments(src), err)
		}
	} else if _, err := root.Read(src...); err != nil {
		return fmt.Errorf("rename source %s: %w", displaySegments(src), err)
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

// RenameEnvVar atomically renames one env variable ciphertext inside its group.
// The ciphertext is moved verbatim, so value, file mode and timestamps are
// preserved.
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

// RenameEnvGroupDir atomically renames an env group directory. The caller is
// responsible for rewriting the group metadata (which embeds the group name)
// inside the moved directory.
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

// RenameTextFile atomically renames one text block ciphertext inside its group.
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

// RenameTextGroup atomically renames a text group directory (text groups carry
// no metadata file, so the directory move is the whole operation).
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

// RenameConfigFile renames one config ciphertext and repoints its index entry.
// The file moves first; if the index rewrite fails the move is rolled back so
// the on-disk index stays authoritative (same discipline as config repair).
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
