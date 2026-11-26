package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
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
	ID             string
	SourceDir      string
	Group          string // 空：新建时用 default；Force 替换时沿用原分组
	Description    string
	DescriptionSet bool // Force 替换时：false 且 Description=="" 则保留原说明；true 则按 Description 覆盖（可清空）
	DefaultInstall *bool
	InstallScope   string
	InstallPath    string
	Targets        []string
	ExtraInfo      string
	ExtraInfoSet   bool   // Force 替换时：false 且 ExtraInfo=="" 则保留原扩展信息；true 则按 ExtraInfo 覆盖（可清空）
	Version        string // 显式指定版本号（优先级高于 SKILL.md frontmatter）
	Note           string // 本次版本发布说明
	// Force 允许替换已存在的同 id skill 或同版本号快照（CreatedAt 与未传入的元数据保留）。
	Force bool
}

// EditOptions 是 Edit 的参数（只修改索引元数据，不解包重打 tar；改组时迁移密文）。
type EditOptions struct {
	ID                string
	Group             *string
	Description       *string
	DefaultInstallSet bool
	DefaultInstall    *bool // DefaultInstallSet=true 且为 nil 表示清除回继承
	InstallScopeSet   bool
	InstallScope      string // InstallScopeSet=true 且为空串表示清除回继承
	InstallPathSet    bool
	InstallPath       string // InstallPathSet=true 且为空串表示清除回继承
	TargetsSet        bool
	Targets           []string // TargetsSet=true 且为空表示清除回继承
	ExtraInfoSet      bool
	ExtraInfo         string // ExtraInfoSet=true 且为空串表示清空扩展信息
}

// GroupOptions 是 AddGroup 的参数。
type GroupOptions struct {
	Name           string
	Description    string
	DefaultInstall *bool
	InstallScope   string
	InstallPath    string
	Targets        []string
}

// GroupEditOptions 是 EditGroup 的参数。
type GroupEditOptions struct {
	Name              string
	Description       *string
	DefaultInstallSet bool
	DefaultInstall    *bool
	InstallScopeSet   bool
	InstallScope      string
	InstallPathSet    bool
	InstallPath       string
	TargetsSet        bool
	Targets           []string
}

// GroupSummary 是 ListGroups 返回的分组摘要。
type GroupSummary struct {
	Group    storage.SkillGroup `json:"group"`
	Count    int                `json:"count"`
	Explicit bool               `json:"explicit"` // 是否已在 index.Groups 中显式登记
}

// ListFilter 是 ListWithPolicy 的过滤条件。
type ListFilter struct {
	Group       string
	DefaultOnly bool
	Scope       string // "" | "global" | "project"
}

// ListedSkill 包含条目元数据与其继承分组后的生效策略。
type ListedSkill struct {
	Entry  storage.SkillEntry           `json:"entry"`
	Policy storage.SkillEffectivePolicy `json:"policy"`
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

	scope, err := storage.ValidateSkillInstallScope(opts.InstallScope)
	if err != nil {
		return nil, err
	}
	opts.InstallScope = scope

	installPath, err := storage.ValidateSkillInstallPath(opts.InstallPath)
	if err != nil {
		return nil, err
	}
	opts.InstallPath = installPath

	targets, err := storage.ValidateSkillTargets(opts.Targets)
	if err != nil {
		return nil, err
	}
	opts.Targets = targets

	extraInfo, err := storage.ValidateSkillExtraInfo(opts.ExtraInfo)
	if err != nil {
		return nil, err
	}
	opts.ExtraInfo = extraInfo

	note, err := storage.ValidateSkillVersionNote(opts.Note)
	if err != nil {
		return nil, err
	}
	opts.Note = note

	if opts.Version != "" {
		ver, err := storage.ValidateSkillVersion(opts.Version)
		if err != nil {
			return nil, err
		}
		if len(opts.ID)+1+len(ver) > storage.MaxSkillStorageKeyLen {
			return nil, fmt.Errorf("skill version storage key %q exceeds %d bytes", storage.SkillVersionStorageKey(opts.ID, ver), storage.MaxSkillStorageKeyLen)
		}
		opts.Version = ver
	}

	if opts.InstallScope == storage.SkillScopeGlobal && opts.InstallPath != "" {
		return nil, fmt.Errorf("install_path cannot be set when install_scope is global")
	}

	// 先在锁外打包校验：失败不占锁，也不碰 vault。
	archive, err := Pack(opts.SourceDir)
	if err != nil {
		return nil, err
	}
	parsed, err := Validate(archive, opts.ID)
	if err != nil {
		return nil, err
	}
	fmVer := ParseVersion(parsed.SkillMD)
	if opts.Version == "" && fmVer != "" {
		normVer, err := storage.ValidateSkillVersion(fmVer)
		if err != nil {
			return nil, fmt.Errorf("SKILL.md frontmatter version: %w", err)
		}
		if len(opts.ID)+1+len(normVer) > storage.MaxSkillStorageKeyLen {
			return nil, fmt.Errorf("skill version storage key %q exceeds %d bytes", storage.SkillVersionStorageKey(opts.ID, normVer), storage.MaxSkillStorageKeyLen)
		}
		fmVer = normVer
	}

	var result *storage.SkillEntry
	err = m.mutate(func(locked *Manager) error {
		entry, err := locked.addLocked(opts, archive, fmVer)
		result = entry
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func nextAutoVersion(existing []storage.SkillVersion) string {
	used := make(map[string]bool, len(existing))
	maxN := 0
	for _, v := range existing {
		used[v.Version] = true
		if len(v.Version) >= 2 && v.Version[0] == 'v' {
			if n, err := strconv.Atoi(v.Version[1:]); err == nil && n > maxN {
				maxN = n
			}
		}
	}
	for n := maxN + 1; ; n++ {
		cand := "v" + strconv.Itoa(n)
		if !used[cand] {
			return cand
		}
	}
}

func (m *Manager) addLocked(opts AddOptions, archive []byte, fmVer string) (*storage.SkillEntry, error) {
	key, err := m.cryptoKey()
	if err != nil {
		return nil, err
	}
	index, err := m.storage.LoadSkillIndexWithKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to load skill index: %w", err)
	}
	existing, exists := index.Skills[opts.ID]
	declaredVer := opts.Version
	if declaredVer == "" {
		declaredVer = fmVer
	}

	group := opts.Group
	if group == "" {
		group = storage.SkillDefaultGroup
		if exists {
			group = existing.NormalizedGroup()
		}
	}

	if exists && !opts.Force {
		if opts.Group != "" && opts.Group != existing.NormalizedGroup() {
			return nil, fmt.Errorf("skill %q already exists in group %q (ids are unique across groups); use --force to move or replace it", opts.ID, existing.NormalizedGroup())
		}
		if declaredVer == "" {
			return nil, fmt.Errorf("skill %q already exists in group %q (ids are unique across groups); use --force to replace it", opts.ID, existing.NormalizedGroup())
		}
		if _, dup := existing.FindVersion(declaredVer); dup {
			return nil, fmt.Errorf("version %q already exists for skill %q (use --force to replace it)", declaredVer, opts.ID)
		}
	}

	archiveHash := sha256Hex(archive)
	archiveSize := len(archive)

	var targetVer string
	switch {
	case !exists:
		targetVer = declaredVer
		if targetVer == "" {
			targetVer = "v1"
		}
	case declaredVer != "":
		targetVer = declaredVer
	default: // exists && declaredVer == "" && opts.Force
		if len(existing.Versions) > 0 {
			if curMeta, ok := existing.FindVersion(existing.CurrentVersion); ok && curMeta.SHA256 == archiveHash && opts.Note == "" {
				targetVer = existing.CurrentVersion
			} else {
				targetVer = nextAutoVersion(existing.Versions)
			}
		} else {
			targetVer = "v1"
		}
	}

	desc := opts.Description
	defaultInstall := opts.DefaultInstall
	installScope := opts.InstallScope
	installPath := opts.InstallPath
	targets := opts.Targets
	extraInfo := opts.ExtraInfo

	if exists {
		if !opts.DescriptionSet && opts.Description == "" {
			desc = existing.Description
		}
		if defaultInstall == nil {
			defaultInstall = existing.DefaultInstall
		}
		if installScope == "" {
			installScope = existing.InstallScope
		}
		if installPath == "" {
			installPath = existing.InstallPath
		}
		if len(targets) == 0 {
			targets = existing.Targets
		}
		if !opts.ExtraInfoSet && opts.ExtraInfo == "" {
			extraInfo = existing.ExtraInfo
		}
	}
	if installScope == storage.SkillScopeGlobal && installPath != "" {
		return nil, fmt.Errorf("install_path cannot be set when install_scope is global")
	}

	now := time.Now()
	versions := append([]storage.SkillVersion(nil), existing.Versions...)

	// 存量无版本 skill 首次版本化升级时，自动把旧归档留存为快照，防止历史丢失。
	if exists && len(existing.Versions) == 0 {
		oldArchive, loadErr := m.storage.LoadSkillWithKey(existing.NormalizedGroup(), opts.ID, key)
		if loadErr == nil {
			oldHash := sha256Hex(oldArchive)
			if oldHash != archiveHash {
				legacyVer := ""
				if oldParsed, vErr := Validate(oldArchive, opts.ID); vErr == nil {
					if parsedVer := ParseVersion(oldParsed.SkillMD); parsedVer != "" {
						if norm, nErr := storage.ValidateSkillVersion(parsedVer); nErr == nil && len(opts.ID)+1+len(norm) <= storage.MaxSkillStorageKeyLen {
							legacyVer = norm
						}
					}
				}
				if legacyVer == "" || legacyVer == targetVer {
					legacyVer = "v0"
					if legacyVer == targetVer {
						legacyVer = "v0-legacy"
					}
				}
				if err := m.storage.SaveSkillVersionWithKey(group, opts.ID, legacyVer, oldArchive, key); err != nil {
					return nil, err
				}
				legacyCreated := existing.UpdatedAt
				if legacyCreated.IsZero() {
					legacyCreated = now
				}
				versions = append(versions, storage.SkillVersion{
					Version:   legacyVer,
					SHA256:    oldHash,
					Size:      len(oldArchive),
					CreatedAt: legacyCreated,
				})
			}
		}
	}

	// 改组时把已有版本快照一并迁入新组（先写新组，索引落盘后再清旧组）。
	if exists && existing.NormalizedGroup() != group {
		for _, v := range existing.Versions {
			verArchive, err := m.storage.LoadSkillVersionWithKey(existing.NormalizedGroup(), opts.ID, v.Version, key)
			if err != nil {
				return nil, err
			}
			if err := m.storage.SaveSkillVersionWithKey(group, opts.ID, v.Version, verArchive, key); err != nil {
				return nil, err
			}
		}
	}

	if err := m.storage.SaveSkillVersionWithKey(group, opts.ID, targetVer, archive, key); err != nil {
		return nil, err
	}
	if err := m.storage.SaveSkillWithKey(group, opts.ID, archive, key); err != nil {
		return nil, err
	}

	replaced := false
	for i := range versions {
		if versions[i].Version == targetVer {
			versions[i].SHA256 = archiveHash
			versions[i].Size = archiveSize
			if opts.Note != "" {
				versions[i].Note = opts.Note
			}
			versions[i].CreatedAt = now
			replaced = true
			break
		}
	}
	if !replaced {
		versions = append(versions, storage.SkillVersion{
			Version:   targetVer,
			SHA256:    archiveHash,
			Size:      archiveSize,
			Note:      opts.Note,
			CreatedAt: now,
		})
	}

	entry := storage.SkillEntry{
		ID:             opts.ID,
		Group:          group,
		Description:    desc,
		DefaultInstall: defaultInstall,
		InstallScope:   installScope,
		InstallPath:    installPath,
		Targets:        targets,
		ExtraInfo:      extraInfo,
		CurrentVersion: targetVer,
		Versions:       versions,
		EncryptedFile:  storage.SkillEncryptedFile(group, opts.ID),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if exists {
		entry.CreatedAt = existing.CreatedAt
	}
	if entry.InstallPath != "" && index.EffectivePolicy(entry).InstallScope != storage.SkillScopeProject {
		return nil, fmt.Errorf("install_path %q requires install_scope=project (on the skill or its group %q)", entry.InstallPath, group)
	}

	index.Skills[opts.ID] = entry
	if err := m.storage.SaveSkillIndexWithKey(index, key); err != nil {
		return nil, fmt.Errorf("failed to save skill index: %w", err)
	}
	// 索引已指向新位置后再清理旧分组里的密文；失败只留下可被 doctor 看到的孤儿文件。
	if exists && existing.NormalizedGroup() != group {
		if err := m.storage.DeleteSkill(existing.NormalizedGroup(), opts.ID); err != nil {
			return nil, err
		}
		for _, v := range existing.Versions {
			if err := m.storage.DeleteSkillVersion(existing.NormalizedGroup(), opts.ID, v.Version); err != nil {
				return nil, err
			}
		}
	}
	return &entry, nil
}

// Edit 就地修改 skill 的索引元数据（不动归档正文；改组时原子迁移密文）。
func (m *Manager) Edit(opts EditOptions) (*storage.SkillEntry, error) {
	if err := storage.ValidateSkillID(opts.ID); err != nil {
		return nil, err
	}
	var newGroup string
	if opts.Group != nil {
		g, err := storage.ValidateSkillGroup(*opts.Group)
		if err != nil {
			return nil, err
		}
		newGroup = g
	}
	var newDesc string
	if opts.Description != nil {
		d, err := storage.ValidateDescription(*opts.Description, true)
		if err != nil {
			return nil, err
		}
		newDesc = d
	}
	var newScope string
	if opts.InstallScopeSet {
		s, err := storage.ValidateSkillInstallScope(opts.InstallScope)
		if err != nil {
			return nil, err
		}
		newScope = s
	}
	var newPath string
	if opts.InstallPathSet {
		p, err := storage.ValidateSkillInstallPath(opts.InstallPath)
		if err != nil {
			return nil, err
		}
		newPath = p
	}
	var newTargets []string
	if opts.TargetsSet {
		t, err := storage.ValidateSkillTargets(opts.Targets)
		if err != nil {
			return nil, err
		}
		newTargets = t
	}
	var newExtra string
	if opts.ExtraInfoSet {
		ex, err := storage.ValidateSkillExtraInfo(opts.ExtraInfo)
		if err != nil {
			return nil, err
		}
		newExtra = ex
	}

	var result *storage.SkillEntry
	err := m.mutate(func(locked *Manager) error {
		key, err := locked.cryptoKey()
		if err != nil {
			return err
		}
		index, err := locked.storage.LoadSkillIndexWithKey(key)
		if err != nil {
			return fmt.Errorf("failed to load skill index: %w", err)
		}
		existing, ok := index.Skills[opts.ID]
		if !ok {
			return fmt.Errorf("skill %q not found", opts.ID)
		}
		updated := existing
		oldGroup := existing.NormalizedGroup()
		targetGroup := oldGroup
		if opts.Group != nil {
			targetGroup = newGroup
			updated.Group = targetGroup
			updated.EncryptedFile = storage.SkillEncryptedFile(targetGroup, opts.ID)
		}
		if opts.Description != nil {
			updated.Description = newDesc
		}
		if opts.DefaultInstallSet {
			updated.DefaultInstall = opts.DefaultInstall
		}
		if opts.InstallScopeSet {
			updated.InstallScope = newScope
			if newScope == storage.SkillScopeGlobal && !opts.InstallPathSet {
				updated.InstallPath = ""
			}
		}
		if opts.InstallPathSet {
			updated.InstallPath = newPath
		}
		if opts.TargetsSet {
			updated.Targets = newTargets
		}
		if opts.ExtraInfoSet {
			updated.ExtraInfo = newExtra
		}
		if updated.InstallScope == storage.SkillScopeGlobal && updated.InstallPath != "" {
			return fmt.Errorf("install_path cannot be set when install_scope is global")
		}
		if updated.InstallPath != "" && index.EffectivePolicy(updated).InstallScope != storage.SkillScopeProject {
			return fmt.Errorf("install_path %q requires install_scope=project (on the skill or its group %q)", updated.InstallPath, targetGroup)
		}

		if targetGroup != oldGroup {
			archive, err := locked.storage.LoadSkillWithKey(oldGroup, opts.ID, key)
			if err != nil {
				return err
			}
			if err := locked.storage.SaveSkillWithKey(targetGroup, opts.ID, archive, key); err != nil {
				return err
			}
			for _, v := range existing.Versions {
				verArchive, err := locked.storage.LoadSkillVersionWithKey(oldGroup, opts.ID, v.Version, key)
				if err != nil {
					return err
				}
				if err := locked.storage.SaveSkillVersionWithKey(targetGroup, opts.ID, v.Version, verArchive, key); err != nil {
					return err
				}
			}
		}
		updated.UpdatedAt = time.Now()
		index.Skills[opts.ID] = updated
		if err := locked.storage.SaveSkillIndexWithKey(index, key); err != nil {
			return fmt.Errorf("failed to save skill index: %w", err)
		}
		if targetGroup != oldGroup {
			if err := locked.storage.DeleteSkill(oldGroup, opts.ID); err != nil {
				return err
			}
			for _, v := range existing.Versions {
				if err := locked.storage.DeleteSkillVersion(oldGroup, opts.ID, v.Version); err != nil {
					return err
				}
			}
		}
		result = &updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ListGroups 返回所有已知分组（显式登记的分组 + 被条目引用的分组）。
func (m *Manager) ListGroups() ([]GroupSummary, error) {
	key, err := m.cryptoKey()
	if err != nil {
		return nil, err
	}
	index, err := m.storage.LoadSkillIndexWithKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to load skill index: %w", err)
	}
	counts := map[string]int{}
	names := map[string]bool{}
	for name := range index.Groups {
		names[name] = true
	}
	for _, entry := range index.Skills {
		g := entry.NormalizedGroup()
		counts[g]++
		names[g] = true
	}
	out := make([]GroupSummary, 0, len(names))
	for name := range names {
		grp, explicit := index.Groups[name]
		if !explicit {
			grp = storage.SkillGroup{Name: name}
		}
		out = append(out, GroupSummary{
			Group:    grp,
			Count:    counts[name],
			Explicit: explicit,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group.Name == storage.SkillDefaultGroup {
			return true
		}
		if out[j].Group.Name == storage.SkillDefaultGroup {
			return false
		}
		return out[i].Group.Name < out[j].Group.Name
	})
	return out, nil
}

// AddGroup 显式登记一个 skill 分组及其默认安装策略。
// 已被存量条目隐式引用但尚未登记元数据的分组也允许通过 AddGroup 补登。
func (m *Manager) AddGroup(opts GroupOptions) (*storage.SkillGroup, error) {
	name, err := storage.ValidateSkillGroup(opts.Name)
	if err != nil {
		return nil, err
	}
	desc, err := storage.ValidateDescription(opts.Description, true)
	if err != nil {
		return nil, err
	}
	scope, err := storage.ValidateSkillInstallScope(opts.InstallScope)
	if err != nil {
		return nil, err
	}
	installPath, err := storage.ValidateSkillInstallPath(opts.InstallPath)
	if err != nil {
		return nil, err
	}
	if scope == storage.SkillScopeGlobal && installPath != "" {
		return nil, fmt.Errorf("install_path cannot be set when install_scope is global")
	}
	if installPath != "" && scope == "" {
		return nil, fmt.Errorf("install_path %q requires install_scope=project", installPath)
	}
	targets, err := storage.ValidateSkillTargets(opts.Targets)
	if err != nil {
		return nil, err
	}

	var result *storage.SkillGroup
	err = m.mutate(func(locked *Manager) error {
		key, err := locked.cryptoKey()
		if err != nil {
			return err
		}
		index, err := locked.storage.LoadSkillIndexWithKey(key)
		if err != nil {
			return fmt.Errorf("failed to load skill index: %w", err)
		}
		if _, exists := index.Groups[name]; exists {
			return fmt.Errorf("skill group %q already exists (use 'senv skill group edit' to update it)", name)
		}
		grp := storage.SkillGroup{
			Name:           name,
			Description:    desc,
			DefaultInstall: opts.DefaultInstall,
			InstallScope:   scope,
			InstallPath:    installPath,
			Targets:        targets,
		}
		index.Groups[name] = grp
		if err := locked.storage.SaveSkillIndexWithKey(index, key); err != nil {
			return fmt.Errorf("failed to save skill index: %w", err)
		}
		result = &grp
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// EditGroup 修改分组元数据与组级默认安装策略。
func (m *Manager) EditGroup(opts GroupEditOptions) (*storage.SkillGroup, error) {
	name, err := storage.ValidateSkillGroup(opts.Name)
	if err != nil {
		return nil, err
	}
	var newDesc string
	if opts.Description != nil {
		d, err := storage.ValidateDescription(*opts.Description, true)
		if err != nil {
			return nil, err
		}
		newDesc = d
	}
	var newScope string
	if opts.InstallScopeSet {
		s, err := storage.ValidateSkillInstallScope(opts.InstallScope)
		if err != nil {
			return nil, err
		}
		newScope = s
	}
	var newPath string
	if opts.InstallPathSet {
		p, err := storage.ValidateSkillInstallPath(opts.InstallPath)
		if err != nil {
			return nil, err
		}
		newPath = p
	}
	var newTargets []string
	if opts.TargetsSet {
		t, err := storage.ValidateSkillTargets(opts.Targets)
		if err != nil {
			return nil, err
		}
		newTargets = t
	}

	var result *storage.SkillGroup
	err = m.mutate(func(locked *Manager) error {
		key, err := locked.cryptoKey()
		if err != nil {
			return err
		}
		index, err := locked.storage.LoadSkillIndexWithKey(key)
		if err != nil {
			return fmt.Errorf("failed to load skill index: %w", err)
		}
		grp, exists := index.Groups[name]
		if !exists {
			// 若组被存量条目引用或是 default 组，允许直接 edit 补登。
			referenced := name == storage.SkillDefaultGroup
			for _, e := range index.Skills {
				if e.NormalizedGroup() == name {
					referenced = true
					break
				}
			}
			if !referenced {
				return fmt.Errorf("skill group %q not found", name)
			}
			grp = storage.SkillGroup{Name: name}
		}
		if opts.Description != nil {
			grp.Description = newDesc
		}
		if opts.DefaultInstallSet {
			grp.DefaultInstall = opts.DefaultInstall
		}
		if opts.InstallScopeSet {
			grp.InstallScope = newScope
			if newScope == storage.SkillScopeGlobal && !opts.InstallPathSet {
				grp.InstallPath = ""
			}
		}
		if opts.InstallPathSet {
			grp.InstallPath = newPath
		}
		if opts.TargetsSet {
			grp.Targets = newTargets
		}
		if grp.InstallScope == storage.SkillScopeGlobal && grp.InstallPath != "" {
			return fmt.Errorf("install_path cannot be set when install_scope is global")
		}
		if grp.InstallPath != "" && grp.InstallScope != storage.SkillScopeProject {
			return fmt.Errorf("install_path %q requires install_scope=project", grp.InstallPath)
		}
		index.Groups[name] = grp
		if err := locked.storage.SaveSkillIndexWithKey(index, key); err != nil {
			return fmt.Errorf("failed to save skill index: %w", err)
		}
		result = &grp
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteGroup 删除空的 skill 分组（default 组不可删除）。
func (m *Manager) DeleteGroup(name string) error {
	norm, err := storage.ValidateSkillGroup(name)
	if err != nil {
		return err
	}
	if norm == storage.SkillDefaultGroup {
		return fmt.Errorf("cannot delete built-in group %q", storage.SkillDefaultGroup)
	}
	return m.mutate(func(locked *Manager) error {
		key, err := locked.cryptoKey()
		if err != nil {
			return err
		}
		index, err := locked.storage.LoadSkillIndexWithKey(key)
		if err != nil {
			return fmt.Errorf("failed to load skill index: %w", err)
		}
		if _, exists := index.Groups[norm]; !exists {
			return fmt.Errorf("skill group %q not found", norm)
		}
		for _, e := range index.Skills {
			if e.NormalizedGroup() == norm {
				return fmt.Errorf("skill group %q is not empty (contains skill %q)", norm, e.ID)
			}
		}
		delete(index.Groups, norm)
		return locked.storage.SaveSkillIndexWithKey(index, key)
	})
}

// ListWithPolicy 返回按 id 排序的 skill 及其生效安装策略。
func (m *Manager) ListWithPolicy(filter ListFilter) ([]ListedSkill, map[string]storage.SkillGroup, error) {
	if filter.Group != "" {
		if _, err := storage.ValidateSkillGroup(filter.Group); err != nil {
			return nil, nil, err
		}
	}
	if filter.Scope != "" {
		s, err := storage.ValidateSkillInstallScope(filter.Scope)
		if err != nil {
			return nil, nil, err
		}
		filter.Scope = s
	}
	key, err := m.cryptoKey()
	if err != nil {
		return nil, nil, err
	}
	index, err := m.storage.LoadSkillIndexWithKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load skill index: %w", err)
	}
	out := make([]ListedSkill, 0, len(index.Skills))
	for _, entry := range index.Skills {
		if filter.Group != "" && entry.NormalizedGroup() != filter.Group {
			continue
		}
		policy := index.EffectivePolicy(entry)
		if filter.DefaultOnly && !policy.DefaultInstall {
			continue
		}
		if filter.Scope != "" && policy.InstallScope != filter.Scope {
			continue
		}
		out = append(out, ListedSkill{Entry: entry, Policy: policy})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Entry.ID < out[j].Entry.ID })
	return out, index.Groups, nil
}

// List 返回按 id 排序的 skill 元数据；group 非空时只返回该分组。
func (m *Manager) List(group string) ([]storage.SkillEntry, error) {
	if group != "" {
		if _, err := storage.ValidateSkillGroup(group); err != nil {
			return nil, err
		}
	}
	key, err := m.cryptoKey()
	if err != nil {
		return nil, err
	}
	index, err := m.storage.LoadSkillIndexWithKey(key)
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
	key, err := m.cryptoKey()
	if err != nil {
		return nil, err
	}
	index, err := m.storage.LoadSkillIndexWithKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to load skill index: %w", err)
	}
	entry, ok := index.Skills[id]
	if !ok {
		return nil, fmt.Errorf("skill %q not found", id)
	}
	return &entry, nil
}

// Detail 是 Show 的结果：索引元数据 + 生效策略 + 归档清单（不含文件内容）。
type Detail struct {
	Entry          storage.SkillEntry
	Policy         storage.SkillEffectivePolicy
	DisplayVersion string // 实际展示的版本（为空表示存量无版本）
	Entries        []Entry
	// ArchiveSize 是入库归档明文字节数（gzip 归档为压缩后大小）。
	ArchiveSize int
	// Compressed 表示归档是否为 gzip 压缩的 tar.gz。
	Compressed bool
	// UncompressedSize 是解压后 ustar tar 的字节数。
	UncompressedSize int
}

// GetWithPolicy 返回单个 skill 的索引元数据与生效安装策略。
func (m *Manager) GetWithPolicy(id string) (*storage.SkillEntry, storage.SkillEffectivePolicy, error) {
	if err := storage.ValidateSkillID(id); err != nil {
		return nil, storage.SkillEffectivePolicy{}, err
	}
	key, err := m.cryptoKey()
	if err != nil {
		return nil, storage.SkillEffectivePolicy{}, err
	}
	index, err := m.storage.LoadSkillIndexWithKey(key)
	if err != nil {
		return nil, storage.SkillEffectivePolicy{}, fmt.Errorf("failed to load skill index: %w", err)
	}
	entry, ok := index.Skills[id]
	if !ok {
		return nil, storage.SkillEffectivePolicy{}, fmt.Errorf("skill %q not found", id)
	}
	return &entry, index.EffectivePolicy(entry), nil
}

// Show 解密并校验 skill 当前活跃归档，返回元数据与文件清单。不返回任何文件内容。
func (m *Manager) Show(id string) (*Detail, error) {
	return m.ShowVersion(id, "")
}

// ShowVersion 解密并校验 skill 指定版本（为空时取当前活跃版）归档，返回元数据与文件清单。
func (m *Manager) ShowVersion(id, version string) (*Detail, error) {
	entry, policy, err := m.GetWithPolicy(id)
	if err != nil {
		return nil, err
	}
	archive, displayVer, err := m.loadArchiveForVersion(entry, version)
	if err != nil {
		return nil, err
	}
	a, err := Validate(archive, id)
	if err != nil {
		return nil, fmt.Errorf("skill %q is invalid: %w", id, err)
	}
	return &Detail{
		Entry:            *entry,
		Policy:           policy,
		DisplayVersion:   displayVer,
		Entries:          a.Entries,
		ArchiveSize:      len(archive),
		Compressed:       a.Compressed,
		UncompressedSize: a.UncompressedSize,
	}, nil
}

// Archive 返回已解密当前活跃版 tar 明文，供展开（apply）使用。调用方展开前必须再用
// Validate 校验——这里只负责取出。
func (m *Manager) Archive(id string) (*storage.SkillEntry, []byte, error) {
	entry, archive, _, err := m.ArchiveVersion(id, "")
	return entry, archive, err
}

// ArchiveVersion 返回指定版本（为空时取当前活跃版）的已解密 tar 明文及实际版本号。
func (m *Manager) ArchiveVersion(id, version string) (*storage.SkillEntry, []byte, string, error) {
	entry, err := m.Get(id)
	if err != nil {
		return nil, nil, "", err
	}
	archive, resolvedVer, err := m.loadArchiveForVersion(entry, version)
	if err != nil {
		return nil, nil, "", err
	}
	return entry, archive, resolvedVer, nil
}

func (m *Manager) loadArchiveForVersion(entry *storage.SkillEntry, version string) ([]byte, string, error) {
	key, err := m.cryptoKey()
	if err != nil {
		return nil, "", err
	}
	if version == "" {
		archive, err := m.storage.LoadSkillWithKey(entry.NormalizedGroup(), entry.ID, key)
		if err != nil {
			return nil, "", err
		}
		return archive, entry.CurrentVersion, nil
	}
	normVer, err := storage.ValidateSkillVersion(version)
	if err != nil {
		return nil, "", err
	}
	if _, ok := entry.FindVersion(normVer); !ok {
		return nil, "", fmt.Errorf("version %q not found for skill %q", normVer, entry.ID)
	}
	archive, err := m.storage.LoadSkillVersionWithKey(entry.NormalizedGroup(), entry.ID, normVer, key)
	if err != nil {
		return nil, "", err
	}
	return archive, normVer, nil
}

// ListVersions 返回 skill 的所有版本快照与当前活跃版本。
func (m *Manager) ListVersions(id string) ([]storage.SkillVersion, string, error) {
	entry, err := m.Get(id)
	if err != nil {
		return nil, "", err
	}
	out := append([]storage.SkillVersion(nil), entry.Versions...)
	return out, entry.CurrentVersion, nil
}

// UseVersion 切换 skill 的当前活跃版本（把指定版本快照写入 <id>.enc 并更新 CurrentVersion）。
func (m *Manager) UseVersion(id, version string) (*storage.SkillEntry, error) {
	if err := storage.ValidateSkillID(id); err != nil {
		return nil, err
	}
	normVer, err := storage.ValidateSkillVersion(version)
	if err != nil {
		return nil, err
	}
	var result *storage.SkillEntry
	err = m.mutate(func(locked *Manager) error {
		key, err := locked.cryptoKey()
		if err != nil {
			return err
		}
		index, err := locked.storage.LoadSkillIndexWithKey(key)
		if err != nil {
			return fmt.Errorf("failed to load skill index: %w", err)
		}
		entry, ok := index.Skills[id]
		if !ok {
			return fmt.Errorf("skill %q not found", id)
		}
		if _, hasVer := entry.FindVersion(normVer); !hasVer {
			return fmt.Errorf("version %q not found for skill %q", normVer, id)
		}
		archive, err := locked.storage.LoadSkillVersionWithKey(entry.NormalizedGroup(), id, normVer, key)
		if err != nil {
			return err
		}
		if _, err := Validate(archive, id); err != nil {
			return fmt.Errorf("skill %q version %q is invalid: %w", id, normVer, err)
		}
		if err := locked.storage.SaveSkillWithKey(entry.NormalizedGroup(), id, archive, key); err != nil {
			return err
		}
		entry.CurrentVersion = normVer
		entry.UpdatedAt = time.Now()
		index.Skills[id] = entry
		if err := locked.storage.SaveSkillIndexWithKey(index, key); err != nil {
			return fmt.Errorf("failed to save skill index: %w", err)
		}
		result = &entry
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// RemoveVersion 删除非当前活跃的版本快照。
func (m *Manager) RemoveVersion(id, version string) (*storage.SkillEntry, error) {
	if err := storage.ValidateSkillID(id); err != nil {
		return nil, err
	}
	normVer, err := storage.ValidateSkillVersion(version)
	if err != nil {
		return nil, err
	}
	var result *storage.SkillEntry
	err = m.mutate(func(locked *Manager) error {
		key, err := locked.cryptoKey()
		if err != nil {
			return err
		}
		index, err := locked.storage.LoadSkillIndexWithKey(key)
		if err != nil {
			return fmt.Errorf("failed to load skill index: %w", err)
		}
		entry, ok := index.Skills[id]
		if !ok {
			return fmt.Errorf("skill %q not found", id)
		}
		if entry.CurrentVersion == normVer {
			return fmt.Errorf("cannot remove active version %q of skill %q; switch to another version first or use 'skill remove %s'", normVer, id, id)
		}
		idx := -1
		for i, v := range entry.Versions {
			if v.Version == normVer {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("version %q not found for skill %q", normVer, id)
		}
		entry.Versions = append(entry.Versions[:idx], entry.Versions[idx+1:]...)
		entry.UpdatedAt = time.Now()
		index.Skills[id] = entry
		if err := locked.storage.SaveSkillIndexWithKey(index, key); err != nil {
			return fmt.Errorf("failed to save skill index: %w", err)
		}
		if err := locked.storage.DeleteSkillVersion(entry.NormalizedGroup(), id, normVer); err != nil {
			return err
		}
		result = &entry
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Remove 从索引与存储中删除 skill 及其全部版本快照。已 apply 到本机的目录不受影响。
func (m *Manager) Remove(id string) error {
	if err := storage.ValidateSkillID(id); err != nil {
		return err
	}
	return m.mutate(func(locked *Manager) error {
		key, err := locked.cryptoKey()
		if err != nil {
			return err
		}
		index, err := locked.storage.LoadSkillIndexWithKey(key)
		if err != nil {
			return fmt.Errorf("failed to load skill index: %w", err)
		}
		entry, ok := index.Skills[id]
		if !ok {
			return fmt.Errorf("skill %q not found", id)
		}
		delete(index.Skills, id)
		if err := locked.storage.SaveSkillIndexWithKey(index, key); err != nil {
			return fmt.Errorf("failed to save skill index: %w", err)
		}
		if err := locked.storage.DeleteSkill(entry.NormalizedGroup(), id); err != nil {
			return err
		}
		for _, v := range entry.Versions {
			if err := locked.storage.DeleteSkillVersion(entry.NormalizedGroup(), id, v.Version); err != nil {
				return err
			}
		}
		return nil
	})
}
