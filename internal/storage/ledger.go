package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/wii/senv/internal/crypto"
)

// 台账（ledger）存储：实体头 ledgers/<grp>/<key>.enc、不可变记录
// ledger_logs/<grp>/<key>.enc；组说明复用 .meta.enc。载荷均为加密 JSON。
const (
	LedgerDirName    = "ledgers"
	LedgerLogDirName = "ledger_logs"
	LedgerFileSuffix = ".enc"
)

func validateLedgerIdentity(group, key string) error {
	if err := validateTextIdentity(group, key); err != nil {
		return err
	}
	return nil
}

// SaveLedgerJSON 把 payload 序列化加密后写入 dir/<group>/<key>.enc（需在
// vault 变更锁内；未持锁时自动加锁）。
func (m *Manager) SaveLedgerJSON(dir, group, key string, payload any, cryptoKey []byte) error {
	if err := validateLedgerIdentity(group, key); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.SaveLedgerJSON(dir, group, key, payload, cryptoKey)
		})
	}
	if err := m.requireCurrentKey(cryptoKey); err != nil {
		return err
	}
	data, err := ToJSON(payload)
	if err != nil {
		return fmt.Errorf("failed to serialize ledger payload: %w", err)
	}
	encrypted, err := crypto.Encrypt(cryptoKey, data)
	if err != nil {
		return fmt.Errorf("failed to encrypt ledger payload: %w", err)
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.EnsureDir([]string{dir, group}, 0o700); err != nil {
		return fmt.Errorf("failed to create ledger group directory: %w", err)
	}
	return root.AtomicWrite([]string{dir, group, key + LedgerFileSuffix}, []byte(encrypted), 0o600)
}

// LoadLedgerJSON 读取并解密 dir/<group>/<key>.enc 到 out。
func (m *Manager) LoadLedgerJSON(dir, group, key string, out any, cryptoKey []byte) error {
	if err := validateLedgerIdentity(group, key); err != nil {
		return err
	}
	_, err := withVaultRead(m, func(locked *Manager) (struct{}, error) {
		if err := locked.requireCurrentKey(cryptoKey); err != nil {
			return struct{}{}, err
		}
		root, err := locked.openDataRoot()
		if err != nil {
			return struct{}{}, err
		}
		defer root.Close()
		encrypted, err := root.Read(dir, group, key+LedgerFileSuffix)
		if err != nil {
			return struct{}{}, fmt.Errorf("ledger %q not found in group %q: %w", key, group, err)
		}
		decrypted, err := crypto.Decrypt(cryptoKey, string(encrypted))
		if err != nil {
			return struct{}{}, fmt.Errorf("failed to decrypt ledger payload: %w", err)
		}
		if err := FromJSON(decrypted, out); err != nil {
			return struct{}{}, fmt.Errorf("failed to parse ledger payload: %w", err)
		}
		return struct{}{}, nil
	})
	return err
}

// ListLedgerKeys 列出 dir/<group>/ 下的条目 key（不含 .meta.enc），已排序。
func (m *Manager) ListLedgerKeys(dir, group string) ([]string, error) {
	return withVaultRead(m, func(locked *Manager) ([]string, error) {
		root, err := locked.openDataRoot()
		if err != nil {
			return nil, err
		}
		defer root.Close()
		files, err := root.ReadDir(dir, group)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrNotExist) {
				return nil, nil
			}
			return nil, err
		}
		keys := make([]string, 0, len(files))
		for _, f := range files {
			if f.IsDir {
				return nil, fmt.Errorf("invalid ledger cache entry type")
			}
			if f.Name == EnvMetaFileName || !strings.HasSuffix(f.Name, LedgerFileSuffix) {
				continue
			}
			keys = append(keys, strings.TrimSuffix(f.Name, LedgerFileSuffix))
		}
		sort.Strings(keys)
		return keys, nil
	})
}

// ListLedgerGroups 列出 dir/ 下的分组名（不存在时返回空）。
func (m *Manager) ListLedgerGroups(dir string) ([]string, error) {
	return withVaultRead(m, func(locked *Manager) ([]string, error) {
		root, err := locked.openDataRoot()
		if err != nil {
			return nil, err
		}
		defer root.Close()
		entries, err := root.ReadDir(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrNotExist) {
				return nil, nil
			}
			return nil, err
		}
		groups := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir {
				groups = append(groups, e.Name)
			}
		}
		sort.Strings(groups)
		return groups, nil
	})
}

// DeleteLedgerFile 删除 dir/<group>/<key>.enc（唯一删除口，仅 delete-record 使用）。
func (m *Manager) DeleteLedgerFile(dir, group, key string) error {
	if err := validateLedgerIdentity(group, key); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.DeleteLedgerFile(dir, group, key) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := removeManagedFile(root, dir, group, key+LedgerFileSuffix); err != nil {
		return fmt.Errorf("failed to delete ledger %q: %w", key, err)
	}
	return nil
}
