package skill

import (
	"fmt"
	"sort"
	"time"

	"github.com/wii/senv/internal/storage"
)

// Manager 管理 vault 中的 skill 条目：索引在数据目录的 skill_index.json，
// 正文是加密的 ustar tar（skills/<group>/<id>.enc）。
type Manager struct {
	storage        *storage.Manager
	password       string
	key            []byte
	mutationLocked bool
}

// NewManager 用口令创建 Manager。
func NewManager(store *storage.Manager, password string) *Manager {
	return &Manager{storage: store, password: password}
}

// NewManagerWithKey 用已派生的 vault key 创建 Manager。
func NewManagerWithKey(store *storage.Manager, key []byte) *Manager {
	return &Manager{storage: store, key: key}
}

func (m *Manager) mutate(fn func(*Manager) error) error {
	if m.mutationLocked {
		return fn(m)
	}
	return m.storage.WithVaultMutation(func(locked *storage.Manager) error {
		clone := *m
		clone.storage = locked
		clone.mutationLocked = true
		return fn(&clone)
	})
}

func (m *Manager) cryptoKey() ([]byte, error) {
	if m.key != nil {
		return m.key, nil
	}
	return m.storage.DeriveKeyFromPassword(m.password)
}

// AddOptions 是 Add 的参数。
type AddOptions struct {
	ID          string
	SourceDir   string
	Group       string // 空：新建时用 default；Force 替换时沿用原分组
	Description string
	// Force 允许替换已存在的同 id skill（整条替换，CreatedAt 保留）。
	Force bool
}

// Add 把 SourceDir 打包、校验并加密入库。id 在整个 vault 内全局唯一。
func (m *Manager) Add(opts AddOptions) (*storage.SkillEntry, error) {
	if err := storage.ValidateSkillID(opts.ID); err != nil {
		return nil, err
	}
	if opts.Group != "" {
		if _, err := storage.ValidateSkillGroup(opts.Group); err != nil {
			return nil, err
		}
	}
	description, err := storage.ValidateDescription(opts.Description, true)
	if err != nil {
		return nil, err
	}
	opts.Description = description

	// 先在锁外打包校验：失败不占锁，也不碰 vault。
	archive, err := Pack(opts.SourceDir)
	if err != nil {
		return nil, err
	}
	if _, err := Validate(archive, opts.ID); err != nil {
		return nil, err
	}

	var result *storage.SkillEntry
	err = m.mutate(func(locked *Manager) error {
		entry, err := locked.addLocked(opts, archive)
		result = entry
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) addLocked(opts AddOptions, archive []byte) (*storage.SkillEntry, error) {
	index, err := m.storage.LoadSkillIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load skill index: %w", err)
	}
	existing, exists := index.Skills[opts.ID]
	if exists && !opts.Force {
		return nil, fmt.Errorf("skill %q already exists in group %q (ids are unique across groups); use --force to replace it", opts.ID, existing.NormalizedGroup())
	}
	group := opts.Group
	if group == "" {
		group = storage.SkillDefaultGroup
		if exists {
			group = existing.NormalizedGroup()
		}
	}

	key, err := m.cryptoKey()
	if err != nil {
		return nil, err
	}
	if err := m.storage.SaveSkillWithKey(group, opts.ID, archive, key); err != nil {
		return nil, err
	}

	now := time.Now()
	entry := storage.SkillEntry{
		ID:            opts.ID,
		Group:         group,
		Description:   opts.Description,
		EncryptedFile: storage.SkillEncryptedFile(group, opts.ID),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if exists {
		entry.CreatedAt = existing.CreatedAt
	}
	index.Skills[opts.ID] = entry
	if err := m.storage.SaveSkillIndex(index); err != nil {
		return nil, fmt.Errorf("failed to save skill index: %w", err)
	}
	// 索引已指向新位置后再清理旧分组里的密文；失败只留下可被 doctor 看到的孤儿文件。
	if exists && existing.NormalizedGroup() != group {
		if err := m.storage.DeleteSkill(existing.NormalizedGroup(), opts.ID); err != nil {
			return nil, err
		}
	}
	return &entry, nil
}

// List 返回按 id 排序的 skill 元数据；group 非空时只返回该分组。
func (m *Manager) List(group string) ([]storage.SkillEntry, error) {
	if group != "" {
		if _, err := storage.ValidateSkillGroup(group); err != nil {
			return nil, err
		}
	}
	index, err := m.storage.LoadSkillIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load skill index: %w", err)
	}
	entries := make([]storage.SkillEntry, 0, len(index.Skills))
	for _, entry := range index.Skills {
		if group != "" && entry.NormalizedGroup() != group {
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}

// Get 返回单个 skill 的索引元数据。
func (m *Manager) Get(id string) (*storage.SkillEntry, error) {
	if err := storage.ValidateSkillID(id); err != nil {
		return nil, err
	}
	index, err := m.storage.LoadSkillIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load skill index: %w", err)
	}
	entry, ok := index.Skills[id]
	if !ok {
		return nil, fmt.Errorf("skill %q not found", id)
	}
	return &entry, nil
}

// Detail 是 Show 的结果：索引元数据 + 归档清单（不含文件内容）。
type Detail struct {
	Entry   storage.SkillEntry
	Entries []Entry
	// ArchiveSize 是 tar 明文字节数。
	ArchiveSize int
}

// Show 解密并校验 skill 归档，返回元数据与文件清单。不返回任何文件内容。
func (m *Manager) Show(id string) (*Detail, error) {
	entry, archive, err := m.loadArchive(id)
	if err != nil {
		return nil, err
	}
	a, err := Validate(archive, id)
	if err != nil {
		return nil, fmt.Errorf("skill %q is invalid: %w", id, err)
	}
	return &Detail{Entry: *entry, Entries: a.Entries, ArchiveSize: len(archive)}, nil
}

// Archive 返回已解密 tar 明文，供展开（apply）使用。调用方展开前必须再用
// Validate 校验——这里只负责取出。
func (m *Manager) Archive(id string) (*storage.SkillEntry, []byte, error) {
	return m.loadArchive(id)
}

func (m *Manager) loadArchive(id string) (*storage.SkillEntry, []byte, error) {
	entry, err := m.Get(id)
	if err != nil {
		return nil, nil, err
	}
	key, err := m.cryptoKey()
	if err != nil {
		return nil, nil, err
	}
	archive, err := m.storage.LoadSkillWithKey(entry.NormalizedGroup(), id, key)
	if err != nil {
		return nil, nil, err
	}
	return entry, archive, nil
}

// Remove 从索引与存储中删除 skill。已 apply 到本机的目录不受影响。
func (m *Manager) Remove(id string) error {
	if err := storage.ValidateSkillID(id); err != nil {
		return err
	}
	return m.mutate(func(locked *Manager) error {
		index, err := locked.storage.LoadSkillIndex()
		if err != nil {
			return fmt.Errorf("failed to load skill index: %w", err)
		}
		entry, ok := index.Skills[id]
		if !ok {
			return fmt.Errorf("skill %q not found", id)
		}
		delete(index.Skills, id)
		if err := locked.storage.SaveSkillIndex(index); err != nil {
			return fmt.Errorf("failed to save skill index: %w", err)
		}
		return locked.storage.DeleteSkill(entry.NormalizedGroup(), id)
	})
}
