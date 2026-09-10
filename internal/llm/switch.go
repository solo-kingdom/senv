// Coding agent 切换：按 agent 原生格式把 senv provider 档案写回配置文件，
// 并维护本机指针。写回遵循统一协议：merge 保留无关键 → 备份原文件 →
// temp+rename 原子替换；指针只在配置写回成功后更新，失败回滚配置。
package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/wii/senv/internal/storage"
)

// CredentialMode 描述该 agent 凭据的归宿。
type CredentialMode int

const (
	// CredentialInline：解密后的 key 明文写入配置文件（文件权限 0600 收敛）。
	CredentialInline CredentialMode = iota
	// CredentialEnvVar：配置只写环境变量名，key 由用户经 senv env 能力暴露。
	CredentialEnvVar
)

// SwitchRequest 携带一次切换的全部落盘输入。Credential 字段按 adapter 的
// CredentialMode 解释：InlineFile 时是解密明文，EnvVarName 时是变量名。
type SwitchRequest struct {
	AgentID       string
	ProviderAlias string
	BaseURL       string
	Model         string
	Credential    string
	// ConfigPath 由 SwitchManager 按 adapter 与目标 home 预先解析，
	// 适配器直接使用，不自行解析 home。
	ConfigPath string
	// tx covers every file the adapter may touch; it is set by SwitchManager.
	tx *configTransaction
}

// AgentAdapter 描述一个受支持的 coding agent 及其配置写回方式。
type AgentAdapter struct {
	ID         string
	Name       string
	ConfigPath func(home string) string
	// ConfigPaths returns every writable path (including ConfigPath).
	ConfigPaths func(home string) []string
	Apply       func(req SwitchRequest) error
	Credential  CredentialMode
	// Protocol 决定档案接入地址写进该 agent 配置时的形态。
	Protocol ProtocolFamily
}

// unsupportedAgents 明确不支持的 agent：cursor 配置无法覆盖（D2），
// zcode 的供应商 schema 由 GUI 写入且无公开文档（D-E 降级）。
var unsupportedAgents = []string{"cursor", "zcode"}

// senvProviderID 返回该档案在各 agent 配置中的供应商标识。alias 已经过
// storage 身份校验，但仍可能含空格等字符，这里收敛为各配置格式安全的
// 键字符（字母数字与连字符）。
func senvProviderID(alias string) string {
	return "senv-" + sanitizeKey(alias)
}

// senvEnvKeyName 返回 senv 管理的凭据环境变量名（codex 等需要 env 的 agent）。
func senvEnvKeyName(alias string) string {
	return "SENV_" + strings.ToUpper(strings.ReplaceAll(sanitizeKey(alias), "-", "_")) + "_API_KEY"
}

func sanitizeKey(alias string) string {
	var b strings.Builder
	for _, r := range alias {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// SupportedAgents 返回全部受支持的 agent 适配器（稳定顺序）。
func SupportedAgents() []AgentAdapter {
	return []AgentAdapter{
		claudeCodeAdapter(),
		codexAdapter(),
		kimiAdapter(),
		piAdapter(),
		opencodeAdapter(),
	}
}

// UnsupportedAgents 返回明确不支持、status 需要展示的 agent id。
func UnsupportedAgents() []string {
	out := make([]string, len(unsupportedAgents))
	copy(out, unsupportedAgents)
	return out
}

// LookupAgent 按 id 查找适配器。
func LookupAgent(id string) (AgentAdapter, bool) {
	for _, a := range SupportedAgents() {
		if a.ID == id {
			return a, true
		}
	}
	return AgentAdapter{}, false
}

// ---------------------------------------------------------------------------
// 通用写回原语
// ---------------------------------------------------------------------------

// applyJSONMerge 读取 path 的 JSON（不存在视为空对象），用 mutate 做结构性
// 修改后以「备份 + temp + rename」原子写回；文件权限收敛 0600。
func applyJSONMerge(path string, mutate func(root map[string]any) error, txs ...*configTransaction) error {
	root := map[string]any{}
	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := mutate(root); err != nil {
		return err
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	data = append(data, '\n')
	if len(txs) > 0 && txs[0] != nil {
		return txs[0].write(path, data)
	}
	return atomicWriteWithBackup(path, data)
}

// ensureSubMap 返回 root 中 key 指向的 map[string]any，缺失或类型不符时
// 创建/替换为空 map 并写回 root。
func ensureSubMap(root map[string]any, key string) map[string]any {
	sub, _ := root[key].(map[string]any)
	if sub == nil {
		sub = map[string]any{}
		root[key] = sub
	}
	return sub
}

// atomicWriteWithBackup 将原文件复制为 <path>.senv-bak，再以 temp+rename
// 原子替换 path。原文件不存在时跳过备份。
func atomicWriteWithBackup(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
		if err := os.WriteFile(path+".senv-bak", existing, 0o600); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".senv-*.tmp")
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
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// restoreFromBackup 用 <path>.senv-bak 恢复原配置；备份不存在时删除 path。
// 恢复错误原样返回，由调用方并入最终错误。
func restoreFromBackup(path string) error {
	backup := path + ".senv-bak"
	data, err := os.ReadFile(backup)
	if err != nil {
		if os.IsNotExist(err) {
			return os.Remove(path)
		}
		return err
	}
	if err := atomicWriteWithBackup(path, data); err != nil {
		return err
	}
	return os.Remove(backup)
}

// tomlEdit 在 TOML 文本上完成两类编辑：顶层键赋值与表块 upsert。
type tomlEdit struct {
	// header 为表头原文（如 `[model_providers.senv-acme]`）；block 为完整
	// 块文本（含 header），缺块时整体追加。
	header string
	block  string
	// topLevel 是需要在首个表头之前生效的顶层键值行（已渲染）。
	topLevel []string
}

// applyTOMLEdits 逐条应用编辑并原子写回。topLevel 行替换同名顶层赋值；
// 不存在时插入到首个表头之前（TOML 顶层键不允许出现在表头之后）。
func applyTOMLMerge(path string, mutate func(root map[string]any) error, txs ...*configTransaction) error {
	src, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	root := map[string]any{}
	if len(src) > 0 {
		if err := toml.Unmarshal(src, &root); err != nil {
			return fmt.Errorf("parse TOML %s: %w", path, err)
		}
	}
	if err := mutate(root); err != nil {
		return err
	}
	data, err := toml.Marshal(root)
	if err != nil {
		return fmt.Errorf("encode TOML %s: %w", path, err)
	}
	if len(txs) > 0 && txs[0] != nil {
		return txs[0].write(path, data)
	}
	return atomicWriteWithBackup(path, data)
}

// setTOMLPath updates one dotted path without interpreting literal values as
// TOML source. Missing intermediate tables are created as maps.
func setTOMLPath(root map[string]any, path []string, value any) {
	current := root
	for _, key := range path[:len(path)-1] {
		next, _ := current[key].(map[string]any)
		if next == nil {
			next = map[string]any{}
			current[key] = next
		}
		current = next
	}
	current[path[len(path)-1]] = value
}

// ---------------------------------------------------------------------------
// 各 agent 适配器
// ---------------------------------------------------------------------------

// claudeCodeAdapter：~/.claude/settings.json 顶层 model + env 块
// （ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN），凭据内联。
func claudeCodeAdapter() AgentAdapter {
	return AgentAdapter{
		ID:       "claude-code",
		Name:     "Claude Code",
		Protocol: ProtocolAnthropic,
		ConfigPath: func(home string) string {
			return filepath.Join(home, ".claude", "settings.json")
		},
		ConfigPaths: func(home string) []string {
			return []string{filepath.Join(home, ".claude", "settings.json")}
		},
		Credential: CredentialInline,
		Apply: func(req SwitchRequest) error {
			return applyJSONMerge(req.ConfigPath, func(root map[string]any) error {
				root["model"] = req.Model
				env := ensureSubMap(root, "env")
				env["ANTHROPIC_BASE_URL"] = req.BaseURL
				env["ANTHROPIC_AUTH_TOKEN"] = req.Credential
				return nil
			}, req.tx)
		},
	}
}

// codexAdapter：~/.codex/config.toml 顶层 model/model_provider +
// [model_providers.<senv id>]；凭据只写 env_key 名，明文不落盘。
func codexAdapter() AgentAdapter {
	return AgentAdapter{
		ID:       "codex",
		Name:     "Codex (OpenAI)",
		Protocol: ProtocolOpenAICompatible,
		ConfigPath: func(home string) string {
			return filepath.Join(home, ".codex", "config.toml")
		},
		ConfigPaths: func(home string) []string {
			return []string{filepath.Join(home, ".codex", "config.toml")}
		},
		Credential: CredentialEnvVar,
		Apply: func(req SwitchRequest) error {
			id := senvProviderID(req.ProviderAlias)
			return applyTOMLMerge(req.ConfigPath, func(root map[string]any) error {
				root["model"] = req.Model
				root["model_provider"] = id
				setTOMLPath(root, []string{"model_providers", id}, map[string]any{
					"name":                 "senv " + req.ProviderAlias,
					"base_url":             req.BaseURL,
					"env_key":              req.Credential,
					"wire_api":             "responses",
					"requires_openai_auth": false,
				})
				return nil
			}, req.tx)
		},
	}
}

// kimiAdapter：~/.kimi-code/config.toml（Kimi Code CLI）——default_model
// 顶层键 + [providers."senv-*"] + [models."senv-*/<model>"]，api_key 内联。
func kimiAdapter() AgentAdapter {
	return AgentAdapter{
		ID:       "kimi",
		Name:     "Kimi Code",
		Protocol: ProtocolOpenAICompatible,
		ConfigPath: func(home string) string {
			return filepath.Join(home, ".kimi-code", "config.toml")
		},
		ConfigPaths: func(home string) []string {
			return []string{filepath.Join(home, ".kimi-code", "config.toml")}
		},
		Credential: CredentialInline,
		Apply: func(req SwitchRequest) error {
			id := senvProviderID(req.ProviderAlias)
			modelAlias := id + "/" + req.Model
			return applyTOMLMerge(req.ConfigPath, func(root map[string]any) error {
				root["default_model"] = modelAlias
				setTOMLPath(root, []string{"providers", id}, map[string]any{
					"type":     "openai",
					"base_url": req.BaseURL,
					"api_key":  req.Credential,
				})
				// max_context_size 为必填项；senv 无法从档案得知真实上下文，
				// 取保守值避免压缩阈值虚高。
				setTOMLPath(root, []string{"models", modelAlias}, map[string]any{
					"provider":         id,
					"model":            req.Model,
					"max_context_size": 131072,
					"display_name":     req.Model,
				})
				return nil
			}, req.tx)
		},
	}
}

// piAdapter：~/.pi/agent/models.json 写 provider 定义（含 apiKey），
// ~/.pi/agent/settings.json 写 defaultProvider/defaultModel。两份文件属于
// 同一事务，第二份失败时第一份由 SwitchManager 统一回滚。
func piAdapter() AgentAdapter {
	return AgentAdapter{
		ID:       "pi",
		Name:     "Pi",
		Protocol: ProtocolOpenAICompatible,
		ConfigPath: func(home string) string {
			return filepath.Join(home, ".pi", "agent", "models.json")
		},
		ConfigPaths: func(home string) []string {
			return []string{
				filepath.Join(home, ".pi", "agent", "models.json"),
				filepath.Join(home, ".pi", "agent", "settings.json"),
			}
		},
		Credential: CredentialInline,
		Apply: func(req SwitchRequest) error {
			id := senvProviderID(req.ProviderAlias)
			if err := applyJSONMerge(req.ConfigPath, func(root map[string]any) error {
				providers := ensureSubMap(root, "providers")
				models := make([]map[string]any, 0, 1)
				models = append(models, map[string]any{"id": req.Model, "name": req.Model})
				providers[id] = map[string]any{
					"baseUrl": req.BaseURL,
					"api":     "openai-completions",
					"apiKey":  req.Credential,
					"models":  models,
				}
				return nil
			}, req.tx); err != nil {
				return err
			}
			settings := filepath.Join(filepath.Dir(req.ConfigPath), "settings.json")
			return applyJSONMerge(settings, func(root map[string]any) error {
				root["defaultProvider"] = id
				root["defaultModel"] = req.Model
				return nil
			}, req.tx)
		},
	}
}

// opencodeAdapter：~/.config/opencode/opencode.json provider.<senv id>
// （npm openai-compatible + options.baseURL/apiKey + models map）+ 顶层
// model = "<id>/<model>"。
func opencodeAdapter() AgentAdapter {
	return AgentAdapter{
		ID:       "opencode",
		Name:     "OpenCode",
		Protocol: ProtocolOpenAICompatible,
		ConfigPath: func(home string) string {
			return filepath.Join(home, ".config", "opencode", "opencode.json")
		},
		ConfigPaths: func(home string) []string {
			return []string{filepath.Join(home, ".config", "opencode", "opencode.json")}
		},
		Credential: CredentialInline,
		Apply: func(req SwitchRequest) error {
			return applyJSONMerge(req.ConfigPath, func(root map[string]any) error {
				id := senvProviderID(req.ProviderAlias)
				providers := ensureSubMap(root, "provider")
				providers[id] = map[string]any{
					"npm":  "@ai-sdk/openai-compatible",
					"name": "senv " + req.ProviderAlias,
					"options": map[string]any{
						"baseURL": req.BaseURL,
						"apiKey":  req.Credential,
					},
					"models": map[string]any{
						req.Model: map[string]any{"name": req.Model},
					},
				}
				root["model"] = id + "/" + req.Model
				return nil
			}, req.tx)
		},
	}
}

// ---------------------------------------------------------------------------
// SwitchManager
// ---------------------------------------------------------------------------

// SwitchManager 组合 vault 档案与指针存储，完成一次完整切换。
type SwitchManager struct {
	providerManager *ProviderManager
	pointerPath     string
	home            string
	homeErr         error
}

// NewSwitchManager 构造切换管理器；pointerPath/home 为空时使用默认位置。
func NewSwitchManager(pm *ProviderManager, pointerPath, home string) *SwitchManager {
	if home == "" {
		resolved, err := os.UserHomeDir()
		if err != nil {
			return &SwitchManager{providerManager: pm, pointerPath: pointerPath, homeErr: fmt.Errorf("resolve home directory: %w", err)}
		}
		home = resolved
	}
	if pointerPath == "" {
		pointerPath = DefaultPointerPath(home)
	}
	return &SwitchManager{providerManager: pm, pointerPath: pointerPath, home: home}
}

// DefaultPointerPath 返回指针文件默认位置（senv 配置目录下）。
func DefaultPointerPath(home string) string {
	return filepath.Join(home, ".config", "senv", "agent-pointers.json")
}

func (sm *SwitchManager) resolvePaths() (string, string, error) {
	if sm.homeErr != nil {
		return "", "", sm.homeErr
	}
	if sm.home == "" || sm.pointerPath == "" {
		return "", "", fmt.Errorf("agent home or pointer path is unresolved")
	}
	return sm.home, sm.pointerPath, nil
}

// resolveCredential 解密档案凭据引用：text: 经 text manager，env: 经 env
// manager。不支持其他前缀（档案写入时已校验，此处兜底）。
func resolveCredential(entry *storage.LLMProviderEntry, pm *ProviderManager) (string, error) {
	kind, rest, _ := strings.Cut(entry.CredentialRef, ":")
	group, key, _ := strings.Cut(rest, "/")
	switch kind {
	case "text":
		tm := pm.textManager()
		value, err := tm.Get(group, key)
		if err != nil {
			return "", fmt.Errorf("decrypt credential %s: %w", entry.CredentialRef, err)
		}
		return value, nil
	case "env":
		em := pm.envManager()
		value, err := em.Get(group, key)
		if err != nil {
			return "", fmt.Errorf("decrypt credential %s: %w", entry.CredentialRef, err)
		}
		return value, nil
	default:
		return "", fmt.Errorf("unsupported credential ref %q", entry.CredentialRef)
	}
}

// SwitchOutput 携带切换结果与给用户的后续提示。
type SwitchOutput struct {
	AgentID   string
	AgentName string
	Provider  string
	Model     string
	// BaseURL 是按该 agent 协议族转换后实际写入配置的接入地址。
	BaseURL       string
	ConfigPath    string
	CredentialEnv string // 非空表示凭据需经该环境变量暴露
}

// Switch 执行完整切换：校验 → 解密凭据 → 适配器写回 → 指针更新（失败回滚）。
func (sm *SwitchManager) Switch(agentID, providerAlias, model string) (*SwitchOutput, error) {
	home, pointerPath, err := sm.resolvePaths()
	if err != nil {
		return nil, err
	}
	adapter, ok := LookupAgent(agentID)
	if !ok {
		return nil, fmt.Errorf("agent %q is not supported; supported: %s", agentID, strings.Join(supportedAgentIDs(), ", "))
	}
	entry, err := sm.providerManager.GetProvider(providerAlias)
	if err != nil {
		return nil, err
	}
	if model == "" {
		model = entry.DefaultModel
		if model == "" {
			if len(entry.Models) == 1 {
				model = entry.Models[0]
			} else {
				return nil, fmt.Errorf("provider %q has no default model; pass --model (available: %s)",
					providerAlias, strings.Join(entry.Models, ", "))
			}
		}
	} else if !sortedContains(entry.Models, model) {
		return nil, fmt.Errorf("model %q is not in provider %q; available: %s",
			model, providerAlias, strings.Join(entry.Models, ", "))
	}

	// api_shape 显式声明时，形态必须与目标 agent 的协议族一致；不兼容时拒绝
	// 且不写任何文件（ADR-0006）。空值走既有行为：只按 agent 协议族归一。
	if shape, err := ParseAPIShape(entry.APIShape); err != nil {
		return nil, err
	} else if shape != "" {
		family, ok := shape.Protocol()
		if !ok || family != adapter.Protocol {
			return nil, fmt.Errorf(
				"provider %q declares api_shape %s, which is incompatible with agent %s (%s); change the provider api_shape or switch to a different provider",
				providerAlias, shape, adapter.Name, DescribeProtocol(adapter.Protocol))
		}
	}

	credential := reqCredential(adapter, providerAlias)
	if adapter.Credential == CredentialInline {
		if credential, err = resolveCredential(entry, sm.providerManager); err != nil {
			return nil, err
		}
	}

	// 档案接入地址统一按 OpenAI 兼容形态落库，写进配置前按该 agent 的协议族
	// 转换（Anthropic 族剥离末段 /v1）。归一幂等，存量档案无需迁移。
	baseURL := baseURLForFamily(entry.BaseURL, adapter.Protocol)

	configPath := adapter.ConfigPath(home)
	txPaths := []string{configPath}
	if adapter.ConfigPaths != nil {
		txPaths = adapter.ConfigPaths(home)
	}
	txPaths = append(txPaths, pointerPath)
	tx, err := newConfigTransaction(txPaths...)
	if err != nil {
		return nil, fmt.Errorf("start %s config transaction: %w", agentID, err)
	}
	defer func() {
		if !tx.committed {
			_ = tx.rollback()
			tx.unlock()
		}
	}()
	req := SwitchRequest{
		AgentID:       agentID,
		ProviderAlias: providerAlias,
		BaseURL:       baseURL,
		Model:         model,
		Credential:    credential,
		ConfigPath:    configPath,
		tx:            tx,
	}
	if err := adapter.Apply(req); err != nil {
		return nil, fmt.Errorf("write %s config: %w", agentID, err)
	}

	pf, err := LoadPointers(pointerPath)
	if err != nil {
		if errors.Is(err, ErrPointerNotFound) {
			pf = &PointerFile{}
		} else {
			return nil, fmt.Errorf("load agent pointers: %w", err)
		}
	}
	pf.Set(agentID, providerAlias, model)
	if err := SavePointers(pointerPath, pf); err != nil {
		if rbErr := tx.rollback(); rbErr != nil {
			return nil, fmt.Errorf("save pointers: %v; restore config also failed: %w", err, rbErr)
		}
		return nil, fmt.Errorf("save pointers: %w (config restored)", err)
	}
	tx.committed = true
	if err := tx.commit(); err != nil {
		return nil, err
	}

	out := &SwitchOutput{
		AgentID:    agentID,
		AgentName:  adapter.Name,
		Provider:   providerAlias,
		Model:      model,
		BaseURL:    baseURL,
		ConfigPath: configPath,
	}
	if adapter.Credential == CredentialEnvVar {
		out.CredentialEnv = credential
	}
	return out, nil
}

// StatusRow 是 status 输出的一行。
type StatusRow struct {
	AgentID    string
	AgentName  string
	Supported  bool
	Pointer    *AgentPointer
	ConfigPath string
}

// Status 汇总全部 agent 的当前指向。指针文件缺失或损坏时已切换行为空、
// 不视为错误（status 不依赖 vault，也应尽量可用；损坏时返回 warning）。
func (sm *SwitchManager) Status() (rows []StatusRow, warning string, err error) {
	home, pointerPath, err := sm.resolvePaths()
	if err != nil {
		return nil, "", err
	}
	pf, err := LoadPointers(pointerPath)
	if err != nil && !errors.Is(err, ErrPointerNotFound) {
		warning = fmt.Sprintf("指针文件损坏（%v），按未切换展示；可删除后重新切换", err)
		pf = &PointerFile{}
	}
	for _, a := range SupportedAgents() {
		row := StatusRow{AgentID: a.ID, AgentName: a.Name, Supported: true, ConfigPath: a.ConfigPath(home)}
		if p, ok := pf.Get(a.ID); ok {
			p := p
			row.Pointer = &p
		}
		rows = append(rows, row)
	}
	for _, id := range UnsupportedAgents() {
		rows = append(rows, StatusRow{AgentID: id, AgentName: id, Supported: false, ConfigPath: unsupportedConfigPath(id, home)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].AgentID < rows[j].AgentID })
	return rows, warning, nil
}

// unsupportedConfigPath 返回不支持 agent 的信息性路径（无写回用途）。
func unsupportedConfigPath(id, home string) string {
	switch id {
	case "cursor":
		return filepath.Join(home, ".cursor", "mcp.json")
	case "zcode":
		return filepath.Join(home, ".zcode", "v2", "config.json")
	default:
		return ""
	}
}

func supportedAgentIDs() []string {
	var ids []string
	for _, a := range SupportedAgents() {
		ids = append(ids, a.ID)
	}
	return ids
}

// reqCredential 计算 EnvVar 模式下的环境变量名（不解密档案）。
func reqCredential(adapter AgentAdapter, alias string) string {
	if adapter.Credential == CredentialEnvVar {
		return senvEnvKeyName(alias)
	}
	return ""
}

func sortedContains(sorted []string, want string) bool {
	for _, v := range sorted {
		if v == want {
			return true
		}
	}
	return false
}
