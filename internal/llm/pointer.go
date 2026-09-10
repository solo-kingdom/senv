// Coding agent 切换指针（driver 决策 D7/D11）：每个 agent 记录最近一次
// 成功切换的 (provider, model)。指针是本机状态，存放于 senv 配置目录而
// 非 vault，不随同步分发；status/switch 以指针为唯一事实源，不回读
// agent 自身的配置文件。
package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wii/senv/internal/storage"
)

// 指针文件与目录权限：指针不含密钥，但沿用仓库惯例收紧权限。
const (
	pointerDirMode  = 0o700
	pointerFileMode = 0o600
)

// 指针相关哨兵错误：调用方用 errors.Is 区分「无指针文件」与「指针损坏」。
var (
	ErrPointerNotFound = errors.New("agent pointer file not found")
	ErrPointerCorrupt  = errors.New("agent pointer file corrupt")
)

// AgentPointer 记录单个 agent 的当前指向：provider、Agent 模型集与默认模型。
type AgentPointer struct {
	Provider string `json:"provider"`
	// Models 是写入该 agent 配置的 Agent 模型集，顺序即写入顺序。
	Models []string `json:"models,omitempty"`
	// DefaultModel 是该 agent 的起始模型，属于 Models。
	DefaultModel string `json:"default_model,omitempty"`
	// Model 是 version 1 遗留字段：仅在读取旧指针时出现，读取后归一进
	// Models/DefaultModel，写回时不再产生。
	Model      string `json:"model,omitempty"`
	SwitchedAt string `json:"switched_at"` // RFC3339
}

// PointerFile 是指针落盘 envelope。
type PointerFile struct {
	Version int                     `json:"version"`
	Agents  map[string]AgentPointer `json:"agents"`
}

// SwitchedAtTime 返回切换时间的本地时间表示。
func (p AgentPointer) SwitchedAtTime() (time.Time, error) {
	t, err := time.Parse(time.RFC3339, p.SwitchedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: bad switched_at %q: %v", ErrPointerCorrupt, p.SwitchedAt, err)
	}
	return t, nil
}

// Get 返回 agent 的指针；不存在时第二个返回值为 false。
func (pf *PointerFile) Get(agentID string) (AgentPointer, bool) {
	if pf == nil || pf.Agents == nil {
		return AgentPointer{}, false
	}
	p, ok := pf.Agents[agentID]
	return p, ok
}

// Set 记录或覆盖 agent 指针，时间戳取当前时刻。
func (pf *PointerFile) Set(agentID, provider string, models []string, defaultModel string) {
	if pf.Agents == nil {
		pf.Agents = map[string]AgentPointer{}
	}
	pf.Agents[agentID] = AgentPointer{
		Provider:     provider,
		Models:       append([]string(nil), models...),
		DefaultModel: defaultModel,
		SwitchedAt:   time.Now().Format(time.RFC3339),
	}
}

// LoadPointers 从 path 读取指针文件；文件不存在返回 ErrPointerNotFound。
func LoadPointers(path string) (*PointerFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrPointerNotFound
		}
		return nil, fmt.Errorf("read agent pointer file: %w", err)
	}
	var pf PointerFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPointerCorrupt, err)
	}
	if pf.Version != 1 {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrPointerCorrupt, pf.Version)
	}
	for id, p := range pf.Agents {
		if _, err := p.SwitchedAtTime(); err != nil {
			return nil, fmt.Errorf("%w: agent %q: %v", ErrPointerCorrupt, id, errors.Unwrap(err))
		}
		// version 1 兼容：只有 model 字段的旧记录读作单元素集。旧记录描述的
		// 就是「配置里确实只有这一个模型」，归一而非要求重新切换。
		if len(p.Models) == 0 && p.Model != "" {
			p.Models = []string{p.Model}
			if p.DefaultModel == "" {
				p.DefaultModel = p.Model
			}
		}
		p.Model = ""
		pf.Agents[id] = p
	}
	return &pf, nil
}

// SavePointers 以 temp+rename 原子写入 path（0600）；失败时不触碰旧文件。
func SavePointers(path string, pf *PointerFile) error {
	if pf == nil {
		return fmt.Errorf("pointer file is nil")
	}
	if pf.Version == 0 {
		pf.Version = 1
	}
	if pf.Version != 1 {
		return fmt.Errorf("pointer file version %d unsupported", pf.Version)
	}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return fmt.Errorf("encode agent pointers: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	// EnsurePrivateDir also tightens a pre-existing 0755 pointer directory
	// before the private pointer file is written.
	if err := storage.EnsurePrivateDir(dir, pointerDirMode); err != nil {
		return fmt.Errorf("create pointer dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".agent-pointers-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(pointerFileMode); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replace agent pointer file: %w", err)
	}
	return nil
}
