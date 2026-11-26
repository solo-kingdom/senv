package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/securefs"
	"github.com/wii/senv/internal/syncschema"
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
	// MaxSkillExtraBytes 是 skill 扩展信息（extra_info）的字节上限（32 KiB）。
	MaxSkillExtraBytes = 32 * 1024

	// SkillVersionSep 是存储键中 skill id 与版本号的分隔符：<id>@<version>。
	SkillVersionSep = "@"
	// MaxSkillVersionLen 是版本号最大长度。
	MaxSkillVersionLen = 64
	// MaxSkillStorageKeyLen 是 <id>@<version> 存储键最大长度（低于服务端 MaxKeyLen=256）。
	MaxSkillStorageKeyLen = 200
	// MaxSkillVersionNoteBytes 是版本说明的最大字节数。
	MaxSkillVersionNoteBytes = 1024

	// SkillScopeGlobal / SkillScopeProject 是 skill 安装作用域枚举。
	SkillScopeGlobal  = "global"
	SkillScopeProject = "project"

	// SkillTargetAgents / SkillTargetClaude 是 skill 安装目标根简称。
	SkillTargetAgents = "agents"
	SkillTargetClaude = "claude"
)

var validSkillVersionRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// SkillGroup 是 skill 分组的元数据与组级默认安装策略。
// 条目未显式设置的字段会继承所属分组的策略。
type SkillGroup struct {
	Name           string   `json:"name"`
	Description    string   `json:"description,omitempty"`
	DefaultInstall *bool    `json:"default_install,omitempty"`
	InstallScope   string   `json:"install_scope,omitempty"` // "" | "global" | "project"
	InstallPath    string   `json:"install_path,omitempty"`  // scope=project 时的推荐根目录（支持 ~ 前缀）
	Targets        []string `json:"targets,omitempty"`       // 可选：agents / claude
}

// SkillVersion 描述一个 skill 的不可变历史版本快照。
type SkillVersion struct {
	Version   string    `json:"version"`
	SHA256    string    `json:"sha256"`
	Size      int       `json:"size"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// SkillEntry 是 skill 索引中的一条元数据。正文（ustar tar）单独加密存放在
// EncryptedFile 指向的密文里；索引本身在 v2 下加密存储。
type SkillEntry struct {
	ID             string         `json:"id"`
	Group          string         `json:"group,omitempty"` // 空值等价于 default
	Description    string         `json:"description,omitempty"`
	DefaultInstall *bool          `json:"default_install,omitempty"` // nil = 继承组，最终兜底 false
	InstallScope   string         `json:"install_scope,omitempty"`   // "" = 继承组，最终兜底 global
	InstallPath    string         `json:"install_path,omitempty"`    // "" = 继承组（仅 project 生效）
	Targets        []string       `json:"targets,omitempty"`         // 空 = 继承组，最终兜底全部
	ExtraInfo      string         `json:"extra_info,omitempty"`      // apply 时追加到 SKILL.md 末尾的实例扩展信息（Markdown，勿放明文密钥）
	CurrentVersion string         `json:"current_version,omitempty"` // 当前活跃版本号（存量未版本化条目为空）
	Versions       []SkillVersion `json:"versions,omitempty"`        // 按创建时间升序记录的版本快照
	EncryptedFile  string         `json:"encrypted_file"`            // 相对数据目录：skills/<group>/<id>.enc
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// FindVersion 按版本号查找快照元数据。
func (e SkillEntry) FindVersion(version string) (SkillVersion, bool) {
	for _, v := range e.Versions {
		if v.Version == version {
			return v, true
		}
	}
	return SkillVersion{}, false
}

// SkillEffectivePolicy 是条目继承分组后的生效安装策略。
type SkillEffectivePolicy struct {
	DefaultInstall          bool     `json:"default_install"`
	DefaultInstallInherited bool     `json:"default_install_inherited,omitempty"`
	InstallScope            string   `json:"install_scope"` // 恒为 global 或 project
	InstallScopeInherited   bool     `json:"install_scope_inherited,omitempty"`
	InstallPath             string   `json:"install_path,omitempty"` // 仅 project 时有效
	InstallPathInherited    bool     `json:"install_path_inherited,omitempty"`
	Targets                 []string `json:"targets"` // 恒非空
	TargetsInherited        bool     `json:"targets_inherited,omitempty"`
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
	Groups map[string]SkillGroup `json:"groups,omitempty"`
	Skills map[string]SkillEntry `json:"skills"`
}

// NewSkillIndex 创建空索引。
func NewSkillIndex() *SkillIndex {
	return &SkillIndex{
		Groups: make(map[string]SkillGroup),
		Skills: make(map[string]SkillEntry),
	}
}

// EffectivePolicy 解析条目继承分组后的生效安装策略。
func (idx *SkillIndex) EffectivePolicy(entry SkillEntry) SkillEffectivePolicy {
	var grp SkillGroup
	var hasGrp bool
	if idx != nil && idx.Groups != nil {
		grp, hasGrp = idx.Groups[entry.NormalizedGroup()]
	}

	var p SkillEffectivePolicy
	switch {
	case entry.DefaultInstall != nil:
		p.DefaultInstall = *entry.DefaultInstall
	case hasGrp && grp.DefaultInstall != nil:
		p.DefaultInstall = *grp.DefaultInstall
		p.DefaultInstallInherited = true
	default:
		p.DefaultInstall = false
		p.DefaultInstallInherited = true
	}

	switch {
	case entry.InstallScope != "":
		p.InstallScope = entry.InstallScope
	case hasGrp && grp.InstallScope != "":
		p.InstallScope = grp.InstallScope
		p.InstallScopeInherited = true
	default:
		p.InstallScope = SkillScopeGlobal
		p.InstallScopeInherited = true
	}

	if p.InstallScope == SkillScopeProject {
		switch {
		case entry.InstallPath != "":
			p.InstallPath = entry.InstallPath
		case hasGrp && grp.InstallPath != "":
			p.InstallPath = grp.InstallPath
			p.InstallPathInherited = true
		}
	}

	switch {
	case len(entry.Targets) > 0:
		p.Targets = append([]string(nil), entry.Targets...)
	case hasGrp && len(grp.Targets) > 0:
		p.Targets = append([]string(nil), grp.Targets...)
		p.TargetsInherited = true
	default:
		p.Targets = []string{SkillTargetAgents, SkillTargetClaude}
		p.TargetsInherited = true
	}
	return p
}

// SkillEncryptedFile 返回 (group,id) 对应的规范密文相对路径。
func SkillEncryptedFile(group, id string) string {
	if group == "" {
		group = SkillDefaultGroup
	}
	return SkillDirName + "/" + group + "/" + id + SkillFileSuffix
}

// SkillVersionStorageKey 返回版本快照在存储层与 syncschema 中的 key（<id>@<version>）。
func SkillVersionStorageKey(id, version string) string {
	return id + SkillVersionSep + version
}

// SkillVersionEncryptedFile 返回 (group,id,version) 对应的规范版本快照密文相对路径。
func SkillVersionEncryptedFile(group, id, version string) string {
	return SkillEncryptedFile(group, SkillVersionStorageKey(id, version))
}

func validateSkillIdentity(group, id string) error {
	if err := ValidateName(group); err != nil {
		return fmt.Errorf("invalid skill group %q: %w", group, err)
	}
	if err := ValidateSkillID(id); err != nil {
		return err
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

// ValidateSkillID 校验 skill id（禁止包含版本分隔符 '@'）。
func ValidateSkillID(id string) error {
	if err := ValidateName(id); err != nil {
		return fmt.Errorf("invalid skill id %q: %w", id, err)
	}
	if strings.Contains(id, SkillVersionSep) {
		return fmt.Errorf("invalid skill id %q: %q is reserved for version separator", id, SkillVersionSep)
	}
	return nil
}

// ValidateSkillVersion 校验并归一 skill 版本号（1–64 字符，匹配 ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$）。
func ValidateSkillVersion(version string) (string, error) {
	v := strings.TrimSpace(version)
	if !validSkillVersionRE.MatchString(v) || ValidateName(v) != nil {
		return "", fmt.Errorf("invalid skill version %q (must match ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$)", version)
	}
	return v, nil
}

// ValidateSkillVersionIdentity 校验 (group, id, version) 三元组与存储键长度。
func ValidateSkillVersionIdentity(group, id, version string) error {
	if err := validateSkillIdentity(group, id); err != nil {
		return err
	}
	ver, err := ValidateSkillVersion(version)
	if err != nil {
		return err
	}
	if ver != version {
		return fmt.Errorf("invalid skill version %q: leading/trailing whitespace not allowed", version)
	}
	if len(id)+1+len(ver) > MaxSkillStorageKeyLen {
		return fmt.Errorf("skill version storage key %q exceeds %d bytes", SkillVersionStorageKey(id, ver), MaxSkillStorageKeyLen)
	}
	return nil
}

// ValidateSkillStorageKey 校验磁盘或 sync 上的 skill 存储键（<id> 或 <id>@<version>）。
func ValidateSkillStorageKey(key string) error {
	if !strings.Contains(key, SkillVersionSep) {
		return ValidateSkillID(key)
	}
	id, ver, _ := strings.Cut(key, SkillVersionSep)
	if err := ValidateSkillID(id); err != nil {
		return err
	}
	normVer, err := ValidateSkillVersion(ver)
	if err != nil {
		return err
	}
	if normVer != ver {
		return fmt.Errorf("invalid skill version %q", ver)
	}
	if len(key) > MaxSkillStorageKeyLen {
		return fmt.Errorf("skill storage key %q exceeds %d bytes", key, MaxSkillStorageKeyLen)
	}
	return nil
}

// ValidateSkillVersionNote 校验并归一版本说明（单行，≤1024 字节，拒绝控制字符）。
func ValidateSkillVersionNote(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	for _, r := range trimmed {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("invalid skill version note: contains control characters")
		}
	}
	if len(trimmed) > MaxSkillVersionNoteBytes {
		return "", fmt.Errorf("skill version note is %d bytes, exceeds the %d byte limit", len(trimmed), MaxSkillVersionNoteBytes)
	}
	return trimmed, nil
}

// ValidateSkillExtraInfo 校验并归一 skill 扩展信息（去除首尾空白，限制最大 32 KiB，拒绝 NUL 字节）。
func ValidateSkillExtraInfo(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if strings.ContainsRune(trimmed, 0) {
		return "", fmt.Errorf("invalid skill extra_info: contains NUL byte")
	}
	if len(trimmed) > MaxSkillExtraBytes {
		return "", fmt.Errorf("skill extra_info is %d bytes, exceeds the %d byte (32 KiB) limit", len(trimmed), MaxSkillExtraBytes)
	}
	return trimmed, nil
}

// ValidateSkillInstallScope 校验并归一安装作用域（空值表示继承）。
func ValidateSkillInstallScope(scope string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(scope))
	switch s {
	case "", SkillScopeGlobal, SkillScopeProject:
		return s, nil
	default:
		return "", fmt.Errorf("invalid skill install scope %q (want global or project)", scope)
	}
}

// ValidateSkillInstallPath 校验并归一推荐安装路径（空值表示未设置）。
// 仅接受 ~ / ~/... 或绝对路径，拒绝控制字符与 .. 穿越段。
func ValidateSkillInstallPath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", nil
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("invalid skill install path %q: contains control characters", raw)
		}
	}
	slashed := filepath.ToSlash(p)
	if slashed != "~" && !strings.HasPrefix(slashed, "~/") && !strings.HasPrefix(slashed, "/") {
		return "", fmt.Errorf("invalid skill install path %q: must be an absolute path or start with ~/", raw)
	}
	for _, seg := range strings.Split(slashed, "/") {
		if seg == ".." {
			return "", fmt.Errorf("invalid skill install path %q: '..' segments are not allowed", raw)
		}
	}
	if slashed == "~" {
		return "~", nil
	}
	if strings.HasPrefix(slashed, "~/") {
		rest := strings.TrimPrefix(slashed, "~/")
		cleaned := filepath.ToSlash(filepath.Clean(rest))
		if cleaned == "." || cleaned == "" {
			return "~", nil
		}
		return "~/" + cleaned, nil
	}
	return filepath.ToSlash(filepath.Clean(slashed)), nil
}

// ValidateSkillTargets 校验并归一目标根简称列表（空列表表示继承）。
func ValidateSkillTargets(targets []string) ([]string, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, raw := range targets {
		for _, part := range strings.Split(raw, ",") {
			p := strings.ToLower(strings.TrimSpace(part))
			if p == "" {
				continue
			}
			switch p {
			case "all":
				return []string{SkillTargetAgents, SkillTargetClaude}, nil
			case SkillTargetAgents, "agent":
				if !seen[SkillTargetAgents] {
					seen[SkillTargetAgents] = true
					out = append(out, SkillTargetAgents)
				}
			case SkillTargetClaude, "claude-code":
				if !seen[SkillTargetClaude] {
					seen[SkillTargetClaude] = true
					out = append(out, SkillTargetClaude)
				}
			default:
				return nil, fmt.Errorf("invalid skill target %q (want agents, claude, or all)", part)
			}
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// normalizeSkillIndex 对索引做失败关闭的校验：任何身份异常都使整个索引不可用，
// 不静默丢条目。返回的是规范化副本（空 group → default，空 encrypted_file → 规范路径）。
func normalizeSkillIndex(index *SkillIndex) (*SkillIndex, error) {
	if index == nil {
		return nil, fmt.Errorf("skill index is nil")
	}
	normalized := NewSkillIndex()
	for mapName, grp := range index.Groups {
		if grp.Name != "" && grp.Name != mapName {
			return nil, fmt.Errorf("skill group identity mismatch: map key %q does not match name %q", mapName, grp.Name)
		}
		name, err := ValidateSkillGroup(mapName)
		if err != nil {
			return nil, err
		}
		desc, err := ValidateDescription(grp.Description, true)
		if err != nil {
			return nil, fmt.Errorf("skill group %q: %w", name, err)
		}
		scope, err := ValidateSkillInstallScope(grp.InstallScope)
		if err != nil {
			return nil, fmt.Errorf("skill group %q: %w", name, err)
		}
		installPath, err := ValidateSkillInstallPath(grp.InstallPath)
		if err != nil {
			return nil, fmt.Errorf("skill group %q: %w", name, err)
		}
		if scope == SkillScopeGlobal && installPath != "" {
			return nil, fmt.Errorf("skill group %q: install_path cannot be set when install_scope is global", name)
		}
		targets, err := ValidateSkillTargets(grp.Targets)
		if err != nil {
			return nil, fmt.Errorf("skill group %q: %w", name, err)
		}
		grp.Name = name
		grp.Description = desc
		grp.InstallScope = scope
		grp.InstallPath = installPath
		grp.Targets = targets
		normalized.Groups[name] = grp
	}
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
		scope, err := ValidateSkillInstallScope(entry.InstallScope)
		if err != nil {
			return nil, fmt.Errorf("skill %q: %w", mapID, err)
		}
		installPath, err := ValidateSkillInstallPath(entry.InstallPath)
		if err != nil {
			return nil, fmt.Errorf("skill %q: %w", mapID, err)
		}
		if scope == SkillScopeGlobal && installPath != "" {
			return nil, fmt.Errorf("skill %q: install_path cannot be set when install_scope is global", mapID)
		}
		targets, err := ValidateSkillTargets(entry.Targets)
		if err != nil {
			return nil, fmt.Errorf("skill %q: %w", mapID, err)
		}
		extraInfo, err := ValidateSkillExtraInfo(entry.ExtraInfo)
		if err != nil {
			return nil, fmt.Errorf("skill %q: %w", mapID, err)
		}
		if err := normalizeSkillVersions(mapID, &entry); err != nil {
			return nil, err
		}
		canonical := SkillEncryptedFile(group, mapID)
		if entry.EncryptedFile == "" {
			entry.EncryptedFile = canonical
		} else if entry.EncryptedFile != canonical {
			return nil, fmt.Errorf("skill index encrypted file mismatch for %q: got %q, want %q", mapID, entry.EncryptedFile, canonical)
		}
		entry.Group = group
		entry.InstallScope = scope
		entry.InstallPath = installPath
		entry.Targets = targets
		entry.ExtraInfo = extraInfo
		normalized.Skills[mapID] = entry
	}
	return normalized, nil
}

func isLowerHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func normalizeSkillVersions(id string, entry *SkillEntry) error {
	if len(entry.Versions) == 0 {
		if entry.CurrentVersion != "" {
			return fmt.Errorf("skill %q: current_version %q set without versions", id, entry.CurrentVersion)
		}
		return nil
	}
	if entry.CurrentVersion == "" {
		return fmt.Errorf("skill %q: current_version is required when versions exist", id)
	}
	seen := make(map[string]bool, len(entry.Versions))
	foundCurrent := false
	versions := make([]SkillVersion, len(entry.Versions))
	for i, v := range entry.Versions {
		normVer, err := ValidateSkillVersion(v.Version)
		if err != nil {
			return fmt.Errorf("skill %q: %w", id, err)
		}
		if normVer != v.Version {
			return fmt.Errorf("skill %q: version %q contains surrounding whitespace", id, v.Version)
		}
		if len(id)+1+len(normVer) > MaxSkillStorageKeyLen {
			return fmt.Errorf("skill %q: version %q exceeds storage key limit", id, normVer)
		}
		if seen[normVer] {
			return fmt.Errorf("skill %q: duplicate version %q", id, normVer)
		}
		seen[normVer] = true
		if v.Size <= 0 || v.Size > MaxSkillPlaintext {
			return fmt.Errorf("skill %q version %q: invalid size %d", id, normVer, v.Size)
		}
		if v.SHA256 != "" && !isLowerHex64(v.SHA256) {
			return fmt.Errorf("skill %q version %q: invalid sha256 %q", id, normVer, v.SHA256)
		}
		note, err := ValidateSkillVersionNote(v.Note)
		if err != nil {
			return fmt.Errorf("skill %q version %q: %w", id, normVer, err)
		}
		v.Version = normVer
		v.Note = note
		versions[i] = v
		if normVer == entry.CurrentVersion {
			foundCurrent = true
		}
	}
	if !foundCurrent {
		return fmt.Errorf("skill %q: current_version %q not found in versions", id, entry.CurrentVersion)
	}
	entry.Versions = versions
	return nil
}

// ParseSkillIndexData decodes a skill index from either legacy plaintext JSON
// or a v2 AAD-encrypted envelope.
func ParseSkillIndexData(data, key []byte, keyEpoch int) (*SkillIndex, error) {
	return ParseSkillIndexDataStrict(data, key, keyEpoch, false)
}

// ParseSkillIndexDataStrict decodes a skill index and, when requireV2 is true,
// rejects legacy plaintext JSON with ErrPlaintextIndexRejected.
func ParseSkillIndexDataStrict(data, key []byte, keyEpoch int, requireV2 bool) (*SkillIndex, error) {
	payload := data
	if crypto.IsV2Ciphertext(string(data)) {
		if len(key) == 0 {
			return nil, ErrEncryptedIndexRequiresKey
		}
		aad := crypto.BuildEntryAAD(syncschema.KindSkillIndex, "", "", keyEpoch)
		plain, err := crypto.DecryptWithAADStrict(key, string(data), aad, requireV2)
		if err != nil {
			return nil, fmt.Errorf("decrypt skill index: %w", err)
		}
		payload = plain
	} else if requireV2 {
		return nil, ErrPlaintextIndexRejected
	}
	var index SkillIndex
	if err := FromJSON(payload, &index); err != nil {
		return nil, fmt.Errorf("parse skill index: %w", err)
	}
	return normalizeSkillIndex(&index)
}

// LoadSkillIndex 读取 skill 索引（无密钥兼容入口）；文件不存在视为空索引。
func (m *Manager) LoadSkillIndex() (*SkillIndex, error) {
	return m.LoadSkillIndexWithKey(nil)
}

// LoadSkillIndexWithKey 读取 skill 索引，并在索引为 v2 密文时使用 key 解密。
func (m *Manager) LoadSkillIndexWithKey(key []byte) (*SkillIndex, error) {
	return withVaultRead(m, func(locked *Manager) (*SkillIndex, error) {
		policy, err := locked.readCryptoPolicyLocked()
		if err != nil {
			return nil, err
		}
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
		return ParseSkillIndexDataStrict(data, key, policy.KeyEpoch, policy.RequireV2)
	})
}

// SaveSkillIndex 校验并原子写入明文 skill 索引（require_v2 开启时拒绝明文）。
func (m *Manager) SaveSkillIndex(index *SkillIndex) error {
	normalized, err := normalizeSkillIndex(index)
	if err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.SaveSkillIndex(normalized) })
	}
	policy, err := m.readCryptoPolicyLocked()
	if err != nil {
		return err
	}
	if policy.RequireV2 {
		return ErrPlaintextIndexRejected
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

// SaveSkillIndexWithKey 校验并使用 v2 AAD 信封加密写入 skill 索引。
func (m *Manager) SaveSkillIndexWithKey(index *SkillIndex, key []byte) error {
	normalized, err := normalizeSkillIndex(index)
	if err != nil {
		return err
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.SaveSkillIndexWithKey(normalized, key) })
	}
	if err := m.requireCurrentKey(key); err != nil {
		return err
	}
	data, err := ToJSON(normalized)
	if err != nil {
		return err
	}
	encrypted, err := m.encryptEntryLocked(key, syncschema.KindSkillIndex, "", "", data)
	if err != nil {
		return err
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.AtomicWrite([]string{SkillIndexFile}, []byte(encrypted), 0o600)
}

// SaveSkillWithKey 加密并写入 skills/<group>/<id>.enc。archive 是 ustar tar 明文，
// 上限 MaxSkillPlaintext。
func (m *Manager) SaveSkillWithKey(group, id string, archive []byte, cryptoKey []byte) error {
	if err := validateSkillIdentity(group, id); err != nil {
		return err
	}
	return m.saveSkillStorageKeyWithKey(group, id, archive, cryptoKey)
}

// SaveSkillVersionWithKey 加密并写入不可变版本快照 skills/<group>/<id>@<version>.enc。
func (m *Manager) SaveSkillVersionWithKey(group, id, version string, archive []byte, cryptoKey []byte) error {
	if err := ValidateSkillVersionIdentity(group, id, version); err != nil {
		return err
	}
	return m.saveSkillStorageKeyWithKey(group, SkillVersionStorageKey(id, version), archive, cryptoKey)
}

func (m *Manager) saveSkillStorageKeyWithKey(group, storageKey string, archive []byte, cryptoKey []byte) error {
	if len(archive) > MaxSkillPlaintext {
		return fmt.Errorf("skill archive is %d bytes, exceeds the %d byte (1 MiB) plaintext limit", len(archive), MaxSkillPlaintext)
	}
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error {
			return locked.saveSkillStorageKeyWithKey(group, storageKey, archive, cryptoKey)
		})
	}
	if err := m.requireCurrentKey(cryptoKey); err != nil {
		return err
	}
	encrypted, err := m.encryptEntryLocked(cryptoKey, syncschema.KindSkill, group, storageKey, archive)
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
	return root.AtomicWrite([]string{SkillDirName, group, storageKey + SkillFileSuffix}, []byte(encrypted), 0o600)
}

// LoadSkillWithKey 读取并解密 skill tar 明文。
func (m *Manager) LoadSkillWithKey(group, id string, cryptoKey []byte) ([]byte, error) {
	if err := validateSkillIdentity(group, id); err != nil {
		return nil, err
	}
	return m.loadSkillStorageKeyWithKey(group, id, cryptoKey)
}

// LoadSkillVersionWithKey 读取并解密指定版本快照 tar 明文。
func (m *Manager) LoadSkillVersionWithKey(group, id, version string, cryptoKey []byte) ([]byte, error) {
	if err := ValidateSkillVersionIdentity(group, id, version); err != nil {
		return nil, err
	}
	return m.loadSkillStorageKeyWithKey(group, SkillVersionStorageKey(id, version), cryptoKey)
}

func (m *Manager) loadSkillStorageKeyWithKey(group, storageKey string, cryptoKey []byte) ([]byte, error) {
	return withVaultRead(m, func(locked *Manager) ([]byte, error) {
		root, err := locked.openDataRoot()
		if err != nil {
			return nil, err
		}
		defer func() { _ = root.Close() }()
		ciphertext, err := root.Read(SkillDirName, group, storageKey+SkillFileSuffix)
		if err != nil {
			return nil, fmt.Errorf("skill %q not found in group %q: %w", storageKey, group, err)
		}
		plaintext, err := locked.decryptEntryLocked(cryptoKey, syncschema.KindSkill, group, storageKey, string(ciphertext))
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt skill %q: %w", storageKey, err)
		}
		if len(plaintext) > MaxSkillPlaintext {
			return nil, fmt.Errorf("skill %q archive exceeds the %d byte plaintext limit", storageKey, MaxSkillPlaintext)
		}
		return plaintext, nil
	})
}

// DeleteSkill 删除一条 skill 密文（不动索引，由调用方维护）。
func (m *Manager) DeleteSkill(group, id string) error {
	if err := validateSkillIdentity(group, id); err != nil {
		return err
	}
	return m.deleteSkillStorageKey(group, id)
}

// DeleteSkillVersion 删除指定版本快照密文（不动索引，由调用方维护）。
func (m *Manager) DeleteSkillVersion(group, id, version string) error {
	if err := ValidateSkillVersionIdentity(group, id, version); err != nil {
		return err
	}
	return m.deleteSkillStorageKey(group, SkillVersionStorageKey(id, version))
}

func (m *Manager) deleteSkillStorageKey(group, storageKey string) error {
	if !m.mutationLocked {
		return m.mutate(func(locked *Manager) error { return locked.deleteSkillStorageKey(group, storageKey) })
	}
	root, err := m.openDataRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := removeManagedFile(root, SkillDirName, group, storageKey+SkillFileSuffix); err != nil {
		return fmt.Errorf("failed to delete skill %q: %w", storageKey, err)
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
			key := strings.TrimSuffix(file.Name, SkillFileSuffix)
			if err := ValidateSkillStorageKey(key); err != nil {
				return nil, fmt.Errorf("invalid historical skill identity: %w", err)
			}
			refs = append(refs, SkillFileRef{Group: group.Name, ID: key})
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
