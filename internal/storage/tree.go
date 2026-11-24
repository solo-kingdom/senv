package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/wii/senv/internal/securefs"
	"github.com/wii/senv/internal/syncschema"
)

const (
	TreeDirName    = "trees"
	TreeFileSuffix = ".enc"
)

func treeFileSegments(group, relPath string) ([]string, []string, error) {
	if err := ValidateName(group); err != nil {
		return nil, nil, fmt.Errorf("invalid tree group %q: %w", group, err)
	}
	if err := syncschema.ValidateRelativePath(relPath); err != nil {
		return nil, nil, fmt.Errorf("invalid tree path %q: %w", relPath, err)
	}
	parts := strings.Split(relPath, "/")
	dirSegs := make([]string, 0, 2+len(parts)-1)
	dirSegs = append(dirSegs, TreeDirName, group)
	dirSegs = append(dirSegs, parts[:len(parts)-1]...)

	fileSegs := make([]string, 0, len(dirSegs)+1)
	fileSegs = append(fileSegs, dirSegs...)
	fileSegs = append(fileSegs, parts[len(parts)-1]+TreeFileSuffix)
	return dirSegs, fileSegs, nil
}

// checkTreePathCollision prevents a path from being both a file and a directory prefix
// (e.g. "a/b" and "a/b/c") inside the same group.
func checkTreePathCollision(root securefs.TrustedRoot, group, relPath string) error {
	parts := strings.Split(relPath, "/")
	// 1. Check if any ancestor prefix is already stored as a regular file (<prefix>.enc).
	for i := 1; i < len(parts); i++ {
		anc := make([]string, 0, 2+i)
		anc = append(anc, TreeDirName, group)
		anc = append(anc, parts[:i-1]...)
		anc = append(anc, parts[i-1]+TreeFileSuffix)
		if _, err := root.Read(anc...); err == nil {
			return fmt.Errorf("path collision: ancestor %q is already a file", strings.Join(parts[:i], "/"))
		} else if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	// 2. Check if target path itself is already a directory.
	targetAsDir := make([]string, 0, 2+len(parts))
	targetAsDir = append(targetAsDir, TreeDirName, group)
	targetAsDir = append(targetAsDir, parts...)
	if entries, err := root.ReadDir(targetAsDir...); err == nil && len(entries) >= 0 {
		return fmt.Errorf("path collision: %q is already a directory", relPath)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// SaveTreeJSON encrypts payload and atomically writes trees/<group>/<relPath>.enc.
func (m *Manager) SaveTreeJSON(group, relPath string, payload any, cryptoKey []byte) error {
	dirSegs, fileSegs, err := treeFileSegments(group, relPath)
	if err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.SaveTreeJSON(group, relPath, payload, cryptoKey)
		})
	}
	if err := m.requireCurrentKey(cryptoKey); err != nil {
		return err
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	if err := checkTreePathCollision(root, group, relPath); err != nil {
		return err
	}
	data, err := ToJSON(payload)
	if err != nil {
		return fmt.Errorf("failed to serialize tree payload: %w", err)
	}
	encrypted, err := m.encryptEntryLocked(cryptoKey, syncschema.KindTree, group, relPath, data)
	if err != nil {
		return fmt.Errorf("failed to encrypt tree payload: %w", err)
	}
	if err := root.EnsureDir(dirSegs, 0o700); err != nil {
		return fmt.Errorf("failed to create tree directory: %w", err)
	}
	return root.AtomicWrite(fileSegs, []byte(encrypted), 0o600)
}

// LoadTreeJSON reads and decrypts trees/<group>/<relPath>.enc into out.
func (m *Manager) LoadTreeJSON(group, relPath string, out any, cryptoKey []byte) error {
	_, fileSegs, err := treeFileSegments(group, relPath)
	if err != nil {
		return err
	}
	_, err = withVaultRead(m, func(locked *Manager) (struct{}, error) {
		if err := locked.requireCurrentKey(cryptoKey); err != nil {
			return struct{}{}, err
		}
		root, err := locked.openDataRoot()
		if err != nil {
			return struct{}{}, err
		}
		defer func() { _ = root.Close() }()
		encrypted, err := root.Read(fileSegs...)
		if err != nil {
			return struct{}{}, fmt.Errorf("tree entry %q not found in group %q: %w", relPath, group, err)
		}
		decrypted, err := locked.decryptEntryLocked(cryptoKey, syncschema.KindTree, group, relPath, string(encrypted))
		if err != nil {
			return struct{}{}, fmt.Errorf("failed to decrypt tree payload: %w", err)
		}
		if err := FromJSON(decrypted, out); err != nil {
			return struct{}{}, fmt.Errorf("failed to parse tree payload: %w", err)
		}
		return struct{}{}, nil
	})
	return err
}

// DeleteTreeFile removes trees/<group>/<relPath>.enc and prunes empty parent directories
// up to (but not including) trees/<group>.
func (m *Manager) DeleteTreeFile(group, relPath string) error {
	_, fileSegs, err := treeFileSegments(group, relPath)
	if err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.DeleteTreeFile(group, relPath)
		})
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := removeManagedFile(root, fileSegs...); err != nil {
		return fmt.Errorf("failed to delete tree entry %q: %w", relPath, err)
	}
	// Prune empty intermediate directories between trees/<group> and file.
	for i := len(fileSegs) - 1; i > 2; i-- {
		dirSegs := fileSegs[:i]
		entries, err := root.ReadDir(dirSegs...)
		if err != nil || len(entries) > 0 {
			break
		}
		if err := root.RemoveTree(dirSegs...); err != nil {
			break
		}
	}
	return nil
}

// ListTreePaths recursively lists all relative paths in trees/<group>/ (sorted).
func (m *Manager) ListTreePaths(group string) ([]string, error) {
	if err := ValidateName(group); err != nil {
		return nil, fmt.Errorf("invalid tree group %q: %w", group, err)
	}
	return withVaultRead(m, func(locked *Manager) ([]string, error) {
		root, err := locked.openDataRoot()
		if err != nil {
			return nil, err
		}
		defer func() { _ = root.Close() }()

		var paths []string
		var walk func(segs []string, prefix string) error
		walk = func(segs []string, prefix string) error {
			entries, err := root.ReadDir(segs...)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			for _, e := range entries {
				if e.IsDir {
					nextSegs := append(append([]string(nil), segs...), e.Name)
					nextPrefix := e.Name
					if prefix != "" {
						nextPrefix = prefix + "/" + e.Name
					}
					if err := walk(nextSegs, nextPrefix); err != nil {
						return err
					}
					continue
				}
				if e.Name == EnvMetaFileName || !strings.HasSuffix(e.Name, TreeFileSuffix) {
					continue
				}
				name := strings.TrimSuffix(e.Name, TreeFileSuffix)
				rel := name
				if prefix != "" {
					rel = prefix + "/" + name
				}
				paths = append(paths, rel)
			}
			return nil
		}
		if err := walk([]string{TreeDirName, group}, ""); err != nil {
			return nil, err
		}
		sort.Strings(paths)
		return paths, nil
	})
}

// ListTreeGroups lists all group directories under trees/.
func (m *Manager) ListTreeGroups() ([]string, error) {
	return m.ListLedgerGroups(TreeDirName)
}

// SaveTreeGroupMeta creates or updates encrypted trees/<group>/.meta.enc.
func (m *Manager) SaveTreeGroupMeta(group, description string, cryptoKey []byte) error {
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid tree group %q: %w", group, err)
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.SaveTreeGroupMeta(group, description, cryptoKey)
		})
	}
	if err := m.requireCurrentKey(cryptoKey); err != nil {
		return err
	}
	desc, err := ValidateDescription(description, false)
	if err != nil {
		return err
	}
	data, err := ToJSON(EnvGroupMeta{Description: desc})
	if err != nil {
		return err
	}
	encrypted, err := m.encryptEntryLocked(cryptoKey, syncschema.KindTreeMeta, group, "", data)
	if err != nil {
		return fmt.Errorf("failed to encrypt tree group meta: %w", err)
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := root.EnsureDir([]string{TreeDirName, group}, 0o700); err != nil {
		return err
	}
	return root.AtomicWrite([]string{TreeDirName, group, EnvMetaFileName}, []byte(encrypted), 0o600)
}

// LoadTreeGroupMeta reads and decrypts trees/<group>/.meta.enc (returns nil, nil if missing).
func (m *Manager) LoadTreeGroupMeta(group string, cryptoKey []byte) (*EnvGroupMeta, error) {
	if err := ValidateName(group); err != nil {
		return nil, fmt.Errorf("invalid tree group %q: %w", group, err)
	}
	return withVaultRead(m, func(locked *Manager) (*EnvGroupMeta, error) {
		if err := locked.requireCurrentKey(cryptoKey); err != nil {
			return nil, err
		}
		root, err := locked.openDataRoot()
		if err != nil {
			return nil, err
		}
		defer func() { _ = root.Close() }()
		encrypted, err := root.Read(TreeDirName, group, EnvMetaFileName)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrNotExist) {
				return nil, nil
			}
			return nil, err
		}
		decrypted, err := locked.decryptEntryLocked(cryptoKey, syncschema.KindTreeMeta, group, "", string(encrypted))
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt tree group meta: %w", err)
		}
		var meta EnvGroupMeta
		if err := FromJSON(decrypted, &meta); err != nil {
			return nil, err
		}
		return &meta, nil
	})
}

// DeleteTreeGroup removes an empty tree group (or non-empty when force is true).
func (m *Manager) DeleteTreeGroup(group string, force bool) error {
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid tree group %q: %w", group, err)
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.DeleteTreeGroup(group, force)
		})
	}
	paths, err := m.ListTreePaths(group)
	if err != nil {
		return err
	}
	if len(paths) > 0 && !force {
		return fmt.Errorf("tree group %q is not empty (%d entries); pass --force to delete", group, len(paths))
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.RemoveTree(TreeDirName, group)
}
