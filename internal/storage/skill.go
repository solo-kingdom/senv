package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/securefs"
)

const (
	// SkillDirName 是 skill 密文在数据目录下的根目录：skills/<group>/<id>.enc。
	SkillDirName    = "skills"
	SkillFileSuffix = ".enc"
	// SkillIndexFile 是 skill 索引文件名。与 config_index.json 不同，它放在
	// 数据目录（随 data/git 同步走）而不是配置目录。
	SkillIndexFile = "skill_index.json"
	// SkillDefaultGroup 是未指定分组时使用的分组。
	SkillDefaultGroup = "default"
	// MaxSkillPlaintext 是加密前 skill tar 归档的大小上限（1 MiB）。
	MaxSkillPlaintext = 1 << 20
)

// SkillEntry 是 skill 索引中的一条元数据。正文（ustar tar）单独加密存放在
// EncryptedFile 指向的密文里；索引本身是明文 JSON，description 可见，可见性
// 与 config_index 相同。
type SkillEntry struct {
	ID            string    `json:"id"`
	Group         string    `json:"group,omitempty"` // 空值等价于 default
	Description   string    `json:"description,omitempty"`
	EncryptedFile string    `json:"encrypted_file"` // 相对数据目录：skills/<group>/<id>.enc
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// NormalizedGroup 返回生效的分组名（空值回落 default）。
func (e SkillEntry) NormalizedGroup() string {
	if e.Group == "" {
		return SkillDefaultGroup
	}
	return e.Group
}

// SkillIndex 是 skill 索引。键为 skill id，id 在整个 vault 内全局唯一（跨分组
// 不可重名），避免 apply 时撞到同一目标路径。
type SkillIndex struct {
	Skills map[string]SkillEntry `json:"skills"`
}

// NewSkillIndex 创建空索引。
func NewSkillIndex() *SkillIndex {
	return &SkillIndex{Skills: make(map[string]SkillEntry)}
}

// SkillEncryptedFile 返回 (group,id) 对应的规范密文相对路径。
func SkillEncryptedFile(group, id string) string {
	if group == "" {
		group = SkillDefaultGroup
	}
	return SkillDirName + "/" + group + "/" + id + SkillFileSuffix
}

func validateSkillIdentity(group, id string) error {
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid skill group %q: %w", group, err)
	}
	if err := ValidateName(id); err != nil {
		return fmt.Errorf("invalid skill id %q: %w", id, err)
	}
	return nil
}

// ValidateSkillGroup 校验 skill 分组名（空值视为 default）。
func ValidateSkillGroup(group string) (string, error) {
	if group == "" {
		group = SkillDefaultGroup
	}
	if err := ValidateName(group); err != nil {
		return "", fmt.Errorf("invalid skill group %q: %w", group, err)
	}
	return group, nil
}

// ValidateSkillID 校验 skill id。
func ValidateSkillID(id string) error {
	if err := ValidateName(id); err != nil {
		return fmt.Errorf("invalid skill id %q: %w", id, err)
	}
	return nil
}

// normalizeSkillIndex 对索引做失败关闭的校验：任何身份异常都使整个索引不可用，
// 不静默丢条目。返回的是规范化副本（空 group → default，空 encrypted_file → 规范路径）。
func normalizeSkillIndex(index *SkillIndex) (*SkillIndex, error) {
	if index == nil {
		return nil, fmt.Errorf("skill index is nil")
	}
	normalized := NewSkillIndex()
	for mapID, entry := range index.Skills {
		if entry.ID != mapID {
			return nil, fmt.Errorf("skill index identity mismatch: map key %q does not match id %q", mapID, entry.ID)
		}
		group, err := ValidateSkillGroup(entry.Group)
		if err != nil {
			return nil, fmt.Errorf("skill %q: %w", mapID, err)
		}
		if err := ValidateSkillID(mapID); err != nil {
			return nil, err
		}
		canonical := SkillEncryptedFile(group, mapID)
		if entry.EncryptedFile == "" {
			entry.EncryptedFile = canonical
		} else if entry.EncryptedFile != canonical {
			return nil, fmt.Errorf("skill index encrypted file mismatch for %q: got %q, want %q", mapID, entry.EncryptedFile, canonical)
		}
		entry.Group = group
		normalized.Skills[mapID] = entry
	}
	return normalized, nil
}

// LoadSkillIndex 读取 skill 索引；文件不存在视为空索引。
func (m *Manager) LoadSkillIndex() (*SkillIndex, error) {
	return withVaultRead(m, func(locked *Manager) (*SkillIndex, error) {
		root, err := locked.openDataRoot()
		if err != nil {
			return nil, err
		}
		defer func() { _ = root.Close() }()
		data, err := root.Read(SkillIndexFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return NewSkillIndex(), nil
			}
			return nil, err
		}
		var index SkillIndex
		if err := FromJSON(data, &index); err != nil {
			return nil, fmt.Errorf("parse skill index: %w", err)
		}
		return normalizeSkillIndex(&index)
	})
}

// SaveSkillIndex 校验并原子写入 skill 索引。
func (m *Manager) SaveSkillIndex(index *SkillIndex) error {
	normalized, err := normalizeSkillIndex(index)
	if err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.SaveSkillIndex(normalized) })
	}
	data, err := ToJSON(normalized)
	if err != nil {
		return err
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.AtomicWrite([]string{SkillIndexFile}, data, 0o600)
}

// SaveSkillWithKey 加密并写入 skills/<group>/<id>.enc。archive 是 ustar tar 明文，
// 上限 MaxSkillPlaintext。
func (m *Manager) SaveSkillWithKey(group, id string, archive []byte, cryptoKey []byte) error {
	if err := validateSkillIdentity(group, id); err != nil {
		return err
	}
	if len(archive) > MaxSkillPlaintext {
		return fmt.Errorf("skill archive is %d bytes, exceeds the %d byte (1 MiB) plaintext limit", len(archive), MaxSkillPlaintext)
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.SaveSkillWithKey(group, id, archive, cryptoKey) })
	}
	if err := m.requireCurrentKey(cryptoKey); err != nil {
		return err
	}
	encrypted, err := crypto.Encrypt(cryptoKey, archive)
	if err != nil {
		return fmt.Errorf("failed to encrypt skill: %w", err)
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := root.EnsureDir([]string{SkillDirName, group}, 0o700); err != nil {
		return fmt.Errorf("failed to create skill group directory: %w", err)
	}
	return root.AtomicWrite([]string{SkillDirName, group, id + SkillFileSuffix}, []byte(encrypted), 0o600)
}

// LoadSkillWithKey 读取并解密 skill tar 明文。
func (m *Manager) LoadSkillWithKey(group, id string, cryptoKey []byte) ([]byte, error) {
	if err := validateSkillIdentity(group, id); err != nil {
		return nil, err
	}
	return withVaultRead(m, func(locked *Manager) ([]byte, error) {
		root, err := locked.openDataRoot()
		if err != nil {
			return nil, err
		}
		defer func() { _ = root.Close() }()
		ciphertext, err := root.Read(SkillDirName, group, id+SkillFileSuffix)
		if err != nil {
			return nil, fmt.Errorf("skill %q not found in group %q: %w", id, group, err)
		}
		plaintext, err := crypto.Decrypt(cryptoKey, string(ciphertext))
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt skill %q: %w", id, err)
		}
		if len(plaintext) > MaxSkillPlaintext {
			return nil, fmt.Errorf("skill %q archive exceeds the %d byte plaintext limit", id, MaxSkillPlaintext)
		}
		return plaintext, nil
	})
}

// DeleteSkill 删除一条 skill 密文（不动索引，由调用方维护）。
func (m *Manager) DeleteSkill(group, id string) error {
	if err := validateSkillIdentity(group, id); err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.DeleteSkill(group, id) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := removeManagedFile(root, SkillDirName, group, id+SkillFileSuffix); err != nil {
		return fmt.Errorf("failed to delete skill %q: %w", id, err)
	}
	return nil
}

// SkillFileRef 标识磁盘上的一份 skill 密文。
type SkillFileRef struct {
	Group string
	ID    string
}

// ListSkillFiles 枚举 skills/ 下的全部密文（不依赖索引，供 doctor/一致性检查用）。
// 身份非法的历史条目失败关闭；分组目录里的非 .enc 普通文件（如 rekey 旁路文件）忽略。
func (m *Manager) ListSkillFiles() ([]SkillFileRef, error) {
	return withVaultRead(m, func(locked *Manager) ([]SkillFileRef, error) {
		root, err := locked.openDataRoot()
		if err != nil {
			return nil, err
		}
		defer func() { _ = root.Close() }()
		return listSkillFilesFromRoot(root)
	})
}

func listSkillFilesFromRoot(root securefs.TrustedRoot) ([]SkillFileRef, error) {
	groups, err := root.ReadDir(SkillDirName)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list skill groups: %w", err)
	}
	var refs []SkillFileRef
	for _, group := range groups {
		if !group.IsDir {
			return nil, fmt.Errorf("invalid skill group entry %q: expected directory", group.Name)
		}
		if err := ValidateName(group.Name); err != nil {
			return nil, fmt.Errorf("invalid historical skill group %q: %w", group.Name, err)
		}
		files, err := root.ReadDir(SkillDirName, group.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to list skill group %q: %w", group.Name, err)
		}
		for _, file := range files {
			if file.IsDir {
				return nil, fmt.Errorf("invalid skill entry %q in group %q: expected file", file.Name, group.Name)
			}
			if !strings.HasSuffix(file.Name, SkillFileSuffix) {
				continue
			}
			id := strings.TrimSuffix(file.Name, SkillFileSuffix)
			if err := validateSkillIdentity(group.Name, id); err != nil {
				return nil, fmt.Errorf("invalid historical skill identity: %w", err)
			}
			refs = append(refs, SkillFileRef{Group: group.Name, ID: id})
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Group != refs[j].Group {
			return refs[i].Group < refs[j].Group
		}
		return refs[i].ID < refs[j].ID
	})
	return refs, nil
}

// hasSkillFiles 报告 skills/ 下是否有任何 .enc 密文（orphan 检测用，尽力而为）。
func hasSkillFiles(dataPath string) bool {
	skillsDir := filepath.Join(dataPath, SkillDirName)
	groups, err := os.ReadDir(skillsDir)
	if err != nil {
		return false
	}
	for _, g := range groups {
		if !g.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(skillsDir, g.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), SkillFileSuffix) {
				return true
			}
		}
	}
	return false
}
