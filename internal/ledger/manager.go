// Package ledger 实现台账：实体头（静态信息 / 状态）与不可变操作记录。
//
// 实体头一个条目（kind=ledger），记录一个条目（kind=ledger_log）；记录追加只
// 新增条目、永不改写已有文件，因此多台设备并发追加不会产生同步冲突。键格式：
//
//	头  ：<type>.<id>
//	记录：<头键>@<YYYYMMDDTHHMMSSZ>-<4hex>
package ledger

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wii/senv/internal/storage"
)

const (
	// SchemaVersion 是台账载荷的结构版本
	SchemaVersion = 1
	// MaxBodyBytes 是单条记录正文上限（远低于服务器单条密文上限）
	MaxBodyBytes = 64 << 10
	// MaxFields 是实体头字段数上限
	MaxFields = 200
	// StatusKeep 是实体头保留的最近状态条数
	StatusKeep = 2
)

var (
	typeRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	idRE   = regexp.MustCompile(`^[^@\s]{1,120}$`)
	tsRE   = regexp.MustCompile(`^(\d{8}T\d{6}Z)-[0-9a-f]{4}$`)
)

// By 记录/状态的客户端自报来源（可伪造，仅作怀疑范围；服务端归因另见 history）
type By struct {
	Host   string `json:"host,omitempty"`
	User   string `json:"user,omitempty"`
	Agent  string `json:"agent,omitempty"`
	Client string `json:"client,omitempty"`
}

// Field 是实体头的有序键值字段
type Field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Link 关联其他实体或仓库路径
type Link struct {
	Rel string `json:"rel"`
	Ref string `json:"ref"`
}

// StatusEntry 是实体头的一条状态观测（覆盖式，只留最近 StatusKeep 条）
type StatusEntry struct {
	At    time.Time `json:"at"`
	By    By        `json:"by,omitempty"`
	Text  string    `json:"text"`
	Level string    `json:"level,omitempty"`
}

// Head 是实体头（kind=ledger）
type Head struct {
	Schema      int           `json:"schema"`
	Type        string        `json:"type"`
	ID          string        `json:"id"`
	Description string        `json:"description,omitempty"`
	Tags        []string      `json:"tags,omitempty"`
	Fields      []Field       `json:"fields,omitempty"`
	Status      []StatusEntry `json:"status,omitempty"`
	Links       []Link        `json:"links,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	// Group 运行时填充（列表/搜索结果用），不写入载荷。
	Group string `json:"-"`
}

// Entity 是记录引用的实体
type Entity struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Record 是不可变操作记录（kind=ledger_log）
type Record struct {
	Schema     int       `json:"schema"`
	Entity     Entity    `json:"entity"`
	At         time.Time `json:"at"`
	Kind       string    `json:"kind"` // op | note | correction
	Summary    string    `json:"summary"`
	Body       string    `json:"body,omitempty"`
	By         By        `json:"by,omitempty"`
	Supersedes []string  `json:"supersedes,omitempty"`
}

// Manager 提供台账读写；key 为 vault 派生密钥
type Manager struct {
	storage *storage.Manager
	key     []byte
}

// NewManager 创建台账管理器
func NewManager(st *storage.Manager, vaultKey []byte) *Manager {
	return &Manager{storage: st, key: vaultKey}
}

// HeadKey 返回实体头键：<type>.<id>
func HeadKey(typ, id string) (string, error) {
	if !typeRE.MatchString(typ) {
		return "", fmt.Errorf("实体类型需为小写字母开头的 kebab-case（收到 %q）", typ)
	}
	if !idRE.MatchString(id) || strings.Contains(id, "@") {
		return "", fmt.Errorf("实体 id 非法（禁 @ 与空白，收到 %q）", id)
	}
	return typ + "." + id, nil
}

// SplitHeadKey 解析实体头键
func SplitHeadKey(key string) (typ, id string, err error) {
	typ, id, ok := strings.Cut(key, ".")
	if !ok || typ == "" || id == "" {
		return "", "", fmt.Errorf("实体键必须是 <type>.<id>（收到 %q）", key)
	}
	if _, err := HeadKey(typ, id); err != nil {
		return "", "", err
	}
	return typ, id, nil
}

// RecordKey 生成记录键：<头键>@<UTC 时间戳>-<4hex>
func RecordKey(headKey string, at time.Time, suffix string) (string, error) {
	if _, _, err := SplitHeadKey(headKey); err != nil {
		return "", err
	}
	if !tsRE.MatchString(at.UTC().Format("20060102T150405Z") + "-" + suffix) {
		return "", fmt.Errorf("记录后缀非法（需 4 位十六进制，收到 %q）", suffix)
	}
	return headKey + "@" + at.UTC().Format("20060102T150405Z") + "-" + suffix, nil
}

func randomSuffix() (string, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// RecordInput 是追加记录的输入
type RecordInput struct {
	Kind       string
	Summary    string
	Body       string
	At         time.Time
	By         By
	Supersedes []string
}

// NewHead 构造一个新的实体头（未落盘）
func NewHead(typ, id, description string, tags []string) (*Head, error) {
	if _, err := HeadKey(typ, id); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Head{Schema: SchemaVersion, Type: typ, ID: id, Description: description, Tags: tags,
		CreatedAt: now, UpdatedAt: now}, nil
}

// SaveHead 写入实体头（新建或覆盖静态信息；并发修改由同步层冲突检测兜底）
func (m *Manager) SaveHead(group string, h *Head) error {
	if h == nil {
		return fmt.Errorf("实体头为空")
	}
	key, err := HeadKey(h.Type, h.ID)
	if err != nil {
		return err
	}
	if len(h.Fields) > MaxFields {
		return fmt.Errorf("字段数超过上限 %d", MaxFields)
	}
	h.Schema = SchemaVersion
	h.UpdatedAt = time.Now().UTC()
	return m.storage.SaveLedgerJSON(storage.LedgerDirName, group, key, h, m.key)
}

// GetHead 读取实体头
func (m *Manager) GetHead(group, headKey string) (*Head, error) {
	var h Head
	if err := m.storage.LoadLedgerJSON(storage.LedgerDirName, group, headKey, &h, m.key); err != nil {
		return nil, err
	}
	h.Group = group
	return &h, nil
}

// ListHeads 列出分组（group 为空 = 全部分组）下的实体头
func (m *Manager) ListHeads(group string) ([]Head, error) {
	groups := []string{group}
	if group == "" {
		var err error
		groups, err = m.storage.ListLedgerGroups(storage.LedgerDirName)
		if err != nil {
			return nil, err
		}
	}
	out := []Head{}
	for _, g := range groups {
		keys, err := m.storage.ListLedgerKeys(storage.LedgerDirName, g)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			h, err := m.GetHead(g, k)
			if err != nil {
				return nil, err
			}
			out = append(out, *h)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Append 追加一条不可变记录（只新增条目，不修改实体头，避免并发写冲突）
func (m *Manager) Append(group, headKey string, in RecordInput) (*Record, string, error) {
	typ, id, err := SplitHeadKey(headKey)
	if err != nil {
		return nil, "", err
	}
	if _, err := m.GetHead(group, headKey); err != nil {
		return nil, "", fmt.Errorf("实体 %s 不存在（先登记实体头）: %w", headKey, err)
	}
	if strings.TrimSpace(in.Summary) == "" {
		return nil, "", fmt.Errorf("记录 summary 不能为空")
	}
	if len(in.Body) > MaxBodyBytes {
		return nil, "", fmt.Errorf("记录正文超过上限 %d 字节", MaxBodyBytes)
	}
	kind := in.Kind
	if kind == "" {
		kind = "op"
	}
	switch kind {
	case "op", "note", "correction":
	default:
		return nil, "", fmt.Errorf("记录类型只支持 op / note / correction（收到 %q）", kind)
	}
	at := in.At
	if at.IsZero() {
		at = time.Now()
	}
	suffix, err := randomSuffix()
	if err != nil {
		return nil, "", err
	}
	key, err := RecordKey(headKey, at, suffix)
	if err != nil {
		return nil, "", err
	}
	rec := &Record{
		Schema: SchemaVersion, Entity: Entity{Type: typ, ID: id}, At: at.UTC(),
		Kind: kind, Summary: in.Summary, Body: in.Body, By: in.By, Supersedes: in.Supersedes,
	}
	if err := m.storage.SaveLedgerJSON(storage.LedgerLogDirName, group, key, rec, m.key); err != nil {
		return nil, "", err
	}
	return rec, key, nil
}

// EditField 修改实体头的一个字段（不存在则追加）；空值表示删除该字段
func (m *Manager) EditField(group, headKey, name, value string) error {
	h, err := m.GetHead(group, headKey)
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("字段名不能为空")
	}
	found := false
	for i := range h.Fields {
		if h.Fields[i].Name == name {
			found = true
			if value == "" {
				h.Fields = append(h.Fields[:i], h.Fields[i+1:]...)
			} else {
				h.Fields[i].Value = value
			}
			break
		}
	}
	if !found && value != "" {
		h.Fields = append(h.Fields, Field{Name: name, Value: value})
	}
	return m.SaveHead(group, h)
}

// SetStatus 覆盖式写入状态（只保留最近 StatusKeep 条）
func (m *Manager) SetStatus(group, headKey string, status StatusEntry) error {
	h, err := m.GetHead(group, headKey)
	if err != nil {
		return err
	}
	if status.At.IsZero() {
		status.At = time.Now()
	}
	h.Status = append([]StatusEntry{status}, h.Status...)
	if len(h.Status) > StatusKeep {
		h.Status = h.Status[:StatusKeep]
	}
	return m.SaveHead(group, h)
}

// RecordWithKey 是带存储键的记录（删除与展示需要）
type RecordWithKey struct {
	Key string
	Record
}

// Log 返回某实体的记录（新到旧，最多 limit 条）
func (m *Manager) Log(group, headKey string, limit int) ([]RecordWithKey, error) {
	if _, _, err := SplitHeadKey(headKey); err != nil {
		return nil, err
	}
	keys, err := m.storage.ListLedgerKeys(storage.LedgerLogDirName, group)
	if err != nil {
		return nil, err
	}
	prefix := headKey + "@"
	var out []RecordWithKey
	for i := len(keys) - 1; i >= 0 && (limit <= 0 || len(out) < limit); i-- {
		if !strings.HasPrefix(keys[i], prefix) {
			continue
		}
		var rec Record
		if err := m.storage.LoadLedgerJSON(storage.LedgerLogDirName, group, keys[i], &rec, m.key); err != nil {
			return nil, err
		}
		out = append(out, RecordWithKey{Key: keys[i], Record: rec})
	}
	return out, nil
}

// Timeline 汇总全部分组的最近记录（按键内时间戳倒序，最多 limit 条）
func (m *Manager) Timeline(limit int, typ string) ([]RecordWithKey, error) {
	groups, err := m.storage.ListLedgerGroups(storage.LedgerLogDirName)
	if err != nil {
		return nil, err
	}
	type kv struct {
		group string
		key   string
	}
	all := []kv{}
	for _, g := range groups {
		keys, err := m.storage.ListLedgerKeys(storage.LedgerLogDirName, g)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			all = append(all, kv{group: g, key: k})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].key > all[j].key })
	out := []RecordWithKey{}
	for _, item := range all {
		if limit > 0 && len(out) >= limit {
			break
		}
		var rec Record
		if err := m.storage.LoadLedgerJSON(storage.LedgerLogDirName, item.group, item.key, &rec, m.key); err != nil {
			return nil, err
		}
		if typ != "" && rec.Entity.Type != typ {
			continue
		}
		out = append(out, RecordWithKey{Key: item.key, Record: rec})
	}
	return out, nil
}

// DeleteRecord 删除一条记录（显式动作；同步为 tombstone，最后版本留在条目历史）
func (m *Manager) DeleteRecord(group, recordKey string) error {
	if _, _, ok := strings.Cut(recordKey, "@"); !ok {
		return fmt.Errorf("记录键需为 <实体键>@<时间戳>-<hex>（收到 %q）", recordKey)
	}
	headKey, rest, _ := strings.Cut(recordKey, "@")
	if _, _, err := SplitHeadKey(headKey); err != nil {
		return err
	}
	if !tsRE.MatchString(rest) {
		return fmt.Errorf("记录键时间戳非法（收到 %q）", rest)
	}
	return m.storage.DeleteLedgerFile(storage.LedgerLogDirName, group, recordKey)
}
