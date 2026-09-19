package storage

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/wii/senv/internal/crypto"
)

// --- Backup file storage methods (parallel to text) ---

func (m *Manager) AddBackupGroup(group string) error {
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid backup group %q: %w", group, err)
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.AddBackupGroup(group) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.EnsureDir([]string{BackupDirName, group}, 0o700)
}

// BackupGroupExists reports whether backups/{group}/ exists.
func (m *Manager) BackupGroupExists(group string) (bool, error) {
	if err := ValidateName(group); err != nil {
		return false, fmt.Errorf("invalid backup group %q: %w", group, err)
	}
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) (bool, error) { return locked.BackupGroupExists(group) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return false, err
	}
	defer func() { _ = root.Close() }()
	_, err = root.ReadDir(BackupDirName, group)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// SaveBackupGroupMetaWithKey writes backups/{group}/.meta.enc.
func (m *Manager) SaveBackupGroupMetaWithKey(group string, meta *EnvGroupMeta, cryptoKey []byte) error {
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid backup group %q: %w", group, err)
	}
	if meta == nil || meta.Name != group {
		return fmt.Errorf("backup group metadata identity mismatch for %q", group)
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.SaveBackupGroupMetaWithKey(group, meta, cryptoKey)
		})
	}
	if err := m.requireCurrentKey(cryptoKey); err != nil {
		return err
	}
	data, err := ToJSON(meta)
	if err != nil {
		return err
	}
	encrypted, err := crypto.Encrypt(cryptoKey, data)
	if err != nil {
		return err
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return writeGroupMetaFile(root, BackupDirName, group, encrypted)
}

// LoadBackupGroupMetaWithKey loads backup group metadata. Missing file returns nil, nil.
func (m *Manager) LoadBackupGroupMetaWithKey(group string, cryptoKey []byte) (*EnvGroupMeta, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) (*EnvGroupMeta, error) {
			return locked.LoadBackupGroupMetaWithKey(group, cryptoKey)
		})
	}
	if err := ValidateName(group); err != nil {
		return nil, fmt.Errorf("invalid backup group %q: %w", group, err)
	}
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	data, err := root.Read(BackupDirName, group, EnvMetaFileName)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	decrypted, err := crypto.Decrypt(cryptoKey, string(data))
	if err != nil {
		return nil, err
	}
	var meta EnvGroupMeta
	if err := FromJSON(decrypted, &meta); err != nil {
		return nil, err
	}
	if meta.Name != group {
		return nil, fmt.Errorf("backup group metadata identity mismatch: requested %q, Name %q", group, meta.Name)
	}
	return &meta, nil
}

func (m *Manager) DeleteBackupGroup(group string) error {
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid backup group %q: %w", group, err)
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.DeleteBackupGroup(group) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.RemoveTree(BackupDirName, group)
}

func (m *Manager) SaveBackupFile(group, key string, entry *BackupEntry, password string) error {
	if err := validateBackupIdentity(group, key); err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("backup entry is nil")
	}
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return err
	}
	return m.SaveBackupFileWithKey(group, key, entry, cryptoKey)
}

func (m *Manager) SaveBackupFileWithKey(group, key string, entry *BackupEntry, cryptoKey []byte) error {
	if err := validateBackupIdentity(group, key); err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("backup entry is nil")
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.SaveBackupFileWithKey(group, key, entry, cryptoKey) })
	}
	if err := m.requireCurrentKey(cryptoKey); err != nil {
		return err
	}
	data, err := ToJSON(entry)
	if err != nil {
		return fmt.Errorf("failed to serialize backup entry: %w", err)
	}
	encryptedData, err := crypto.Encrypt(cryptoKey, data)
	if err != nil {
		return fmt.Errorf("failed to encrypt backup entry: %w", err)
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := root.EnsureDir([]string{BackupDirName, group}, 0o700); err != nil {
		return fmt.Errorf("failed to create backup group directory: %w", err)
	}
	return root.AtomicWrite([]string{BackupDirName, group, key + BackupFileSuffix}, []byte(encryptedData), 0o600)
}

func (m *Manager) LoadBackupFile(group, key string, password string) (*BackupEntry, error) {
	if err := validateBackupIdentity(group, key); err != nil {
		return nil, err
	}
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return nil, err
	}
	return m.LoadBackupFileWithKey(group, key, cryptoKey)
}

func (m *Manager) LoadBackupFileWithKey(group, key string, cryptoKey []byte) (*BackupEntry, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) (*BackupEntry, error) {
			return locked.LoadBackupFileWithKey(group, key, cryptoKey)
		})
	}
	if err := validateBackupIdentity(group, key); err != nil {
		return nil, err
	}
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	encryptedData, err := root.Read(BackupDirName, group, key+BackupFileSuffix)
	if err != nil {
		return nil, fmt.Errorf("backup %q not found in group %q: %w", key, group, err)
	}
	decryptedData, err := crypto.Decrypt(cryptoKey, string(encryptedData))
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt backup entry: %w", err)
	}
	var entry BackupEntry
	if err := FromJSON(decryptedData, &entry); err != nil {
		return nil, fmt.Errorf("failed to parse backup entry: %w", err)
	}
	return &entry, nil
}

func (m *Manager) DeleteBackupFile(group, key string) error {
	if err := validateBackupIdentity(group, key); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.DeleteBackupFile(group, key) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := removeManagedFile(root, BackupDirName, group, key+BackupFileSuffix); err != nil {
		return fmt.Errorf("failed to delete backup %q: %w", key, err)
	}
	return nil
}

func (m *Manager) ListBackupFiles(group string) ([]string, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) ([]string, error) { return locked.ListBackupFiles(group) })
	}
	if err := ValidateName(group); err != nil {
		return nil, fmt.Errorf("invalid backup group %q: %w", group, err)
	}
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	entries, err := root.ReadDir(BackupDirName, group)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list backup group %q: %w", group, err)
	}
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Name == EnvMetaFileName {
			if entry.IsDir {
				return nil, fmt.Errorf("invalid backup metadata entry in group %q", group)
			}
			continue
		}
		if entry.IsDir || !strings.HasSuffix(entry.Name, BackupFileSuffix) {
			return nil, fmt.Errorf("invalid historical backup entry %q in group %q", entry.Name, group)
		}
		key := strings.TrimSuffix(entry.Name, BackupFileSuffix)
		if err := validateBackupIdentity(group, key); err != nil {
			return nil, fmt.Errorf("invalid historical backup identity: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func (m *Manager) ListBackupGroups() ([]string, error) {
	if !m.mutationLocked {
		return withVaultRead(m, func(locked *Manager) ([]string, error) { return locked.ListBackupGroups() })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	entries, err := root.ReadDir(BackupDirName)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list backup groups: %w", err)
	}
	groups := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir {
			return nil, fmt.Errorf("invalid backup group entry %q: expected directory", entry.Name)
		}
		if err := ValidateName(entry.Name); err != nil {
			return nil, fmt.Errorf("invalid historical backup group %q: %w", entry.Name, err)
		}
		groups = append(groups, entry.Name)
	}
	return groups, nil
}

// BackupVaultFile 是单趟装载出的一个 backup 条目：文件名解析出的 key 与解密后
// 的条目内容（BackupEntry 自身不记录 key）。
type BackupVaultFile struct {
	Key   string
	Entry *BackupEntry
}

// BackupVaultSnapshot 是单趟 vault 读内装载的全量 backup 视图：全部分组（含空
// 组，Groups 保持目录枚举序）及各组解密后的条目。校验与解密语义与逐组
// ListBackupGroups/ListBackupFiles、逐条 LoadBackupFileWithKey 完全一致（fail-closed）。
//
// 条目级失败（读取/解密/解析某个条目文件）按组降级：该组不计入 Entries、
// 原因记入 Errors，但 KeyCount 保留列出的文件数——与逐组消费方「List 失败
// → 该组置空、分组仍列出」的语义等价；目录枚举与身份校验失败仍然整体失败。
type BackupVaultSnapshot struct {
	Groups   []string
	KeyCount map[string]int
	Entries  map[string][]BackupVaultFile
	Errors   map[string]error
}

// LoadBackupVault 使用密码加载全部 backup 分组与条目（单趟锁内批量装载）。
func (m *Manager) LoadBackupVault(password string) (*BackupVaultSnapshot, error) {
	cryptoKey, err := m.deriveKeyFromPassword(password)
	if err != nil {
		return nil, err
	}
	return m.LoadBackupVaultWithKey(cryptoKey)
}

// LoadBackupVaultWithKey 在单次 vault 读锁内、共享单个 data root 加载全部
// text 分组与条目——一次 flock、一次 rekey 清算、一次 root 开闭，替代
// 「每组一次 ListBackupFiles + 每条目一次 LoadBackupFile」的 N 次锁路径
// （与 LoadEnvVaultWithKey 同构，tui-startup-perf D2）。
func (m *Manager) LoadBackupVaultWithKey(cryptoKey []byte) (*BackupVaultSnapshot, error) {
	return withVaultRead(m, func(locked *Manager) (*BackupVaultSnapshot, error) {
		return locked.loadBackupVaultWithKey(cryptoKey)
	})
}

func (m *Manager) loadBackupVaultWithKey(cryptoKey []byte) (*BackupVaultSnapshot, error) {
	root, err := m.openDataRoot()
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	groupDirs, err := root.ReadDir(BackupDirName)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("list backup groups: %w", err)
	}
	snap := &BackupVaultSnapshot{
		KeyCount: make(map[string]int, len(groupDirs)),
		Entries:  make(map[string][]BackupVaultFile, len(groupDirs)),
		Errors:   make(map[string]error),
	}
	for _, gd := range groupDirs {
		if !gd.IsDir {
			return nil, fmt.Errorf("invalid backup group entry %q: expected directory", gd.Name)
		}
		if err := ValidateName(gd.Name); err != nil {
			return nil, fmt.Errorf("invalid historical backup group %q: %w", gd.Name, err)
		}
		snap.Groups = append(snap.Groups, gd.Name)

		files, err := root.ReadDir(BackupDirName, gd.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to list backup group %q: %w", gd.Name, err)
		}
		// 文件清单与身份校验沿用逐组路径的 fail-closed 语义：任何非法
		// 历史条目使整个快照失败（对应 ListGroups 阶段失败）。组说明
		// 存在 .meta.enc，与 ListBackupFiles 一样跳过，不计入条目。
		type listedFile struct {
			name string
			key  string
		}
		content := make([]listedFile, 0, len(files))
		for _, f := range files {
			if f.Name == EnvMetaFileName {
				if f.IsDir {
					return nil, fmt.Errorf("invalid backup metadata entry in group %q", gd.Name)
				}
				continue
			}
			if f.IsDir || !strings.HasSuffix(f.Name, BackupFileSuffix) {
				return nil, fmt.Errorf("invalid historical backup entry %q in group %q", f.Name, gd.Name)
			}
			key := strings.TrimSuffix(f.Name, BackupFileSuffix)
			if err := validateBackupIdentity(gd.Name, key); err != nil {
				return nil, fmt.Errorf("invalid historical backup identity: %w", err)
			}
			content = append(content, listedFile{name: f.Name, key: key})
		}
		snap.KeyCount[gd.Name] = len(content)
		var groupFiles []BackupVaultFile
		var groupErr error
		for _, f := range content {
			encryptedData, err := root.Read(BackupDirName, gd.Name, f.name)
			if err != nil {
				groupErr = fmt.Errorf("failed to read backup %q in group %q: %w", f.key, gd.Name, err)
				break
			}
			decryptedData, err := crypto.Decrypt(cryptoKey, string(encryptedData))
			if err != nil {
				groupErr = fmt.Errorf("failed to decrypt backup entry: %w", err)
				break
			}
			var entry BackupEntry
			if err := FromJSON(decryptedData, &entry); err != nil {
				groupErr = fmt.Errorf("failed to parse backup entry: %w", err)
				break
			}
			groupFiles = append(groupFiles, BackupVaultFile{Key: f.key, Entry: &entry})
		}
		// 条目级失败按组降级（对应逐组 List 失败后消费方把该组置空）：分组
		// 保留列出，仅该组条目缺失。
		if groupErr != nil {
			snap.Errors[gd.Name] = groupErr
			continue
		}
		snap.Entries[gd.Name] = groupFiles
	}
	return snap, nil
}
