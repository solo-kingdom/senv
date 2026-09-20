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
	"slices"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/wii/senv/internal/agentcfg"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/ref"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
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
	// Models 是本次写入的 Agent 模型集（保序、非空）。
	Models []string
	// DefaultModel 是 agent 的起始模型，属于 Models。
	DefaultModel string
	Credential   string
	// ConfigPath 由 SwitchManager 按 adapter 与目标 home 预先解析，
	// 适配器直接使用，不自行解析 home。
	ConfigPath string
	// Home 是目标 agent 的 home 目录，供需要额外落盘位置的适配器（codex
	// 的 catalog 文件）拼接路径。
	Home string
	// ModelMetadata 是本次模型集在模型目录里的元数据（可缺失），供各适配器
	// 填充 agent 原生格式需要的字段。
	ModelMetadata map[string]ModelMetadata
	// APIShape 是档案显式声明的接口形态（已归一；空表示未声明）。kimi/pi/
	// opencode 用它在 OpenAI 兼容族内选 chat / responses 线协议（ADR-0006 的
	// 落点）；codex 只讲 responses，不消费该值。
	APIShape string
	// PriorProvider/PriorModels 是上一次成功切换的本机记录，作为清理差集的
	// 依据；首次切换时为零值。
	PriorProvider string
	PriorModels   []string
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
	// OwnedArtifacts 返回某 provider alias 派生的 senv 自有文件（配置目录之外，
	// 如 codex 的 model catalog）。切换把它们纳入同一事务；alias 不再被任何
	// agent 指针指向时删除。无派生文件的 agent 为 nil。
	OwnedArtifacts func(home, alias string) []string
	Apply          func(req SwitchRequest) error
	Credential     CredentialMode
	// Protocol 决定档案接入地址写进该 agent 配置时的形态。
	Protocol ProtocolFamily
}

// senvProviderID 返回该档案在各 agent 配置中的供应商标识。alias 已经过
// storage 身份校验，但仍可能含空格等字符，这里收敛为各配置格式安全的
// 键字符（字母数字与连字符）。
func senvProviderID(alias string) string {
	return "senv-" + sanitizeKey(alias)
}

// senvEnvKeyName 返回 senv 派生的凭据环境变量名：只在凭据没有可复用的
// 环境变量名时使用（`text:` 引用，见 codexEnvPlanFor）。
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

// parseCredentialRef 把档案凭据引用拆解为 kind/group/key（env:<g>/<k> 或
// text:<g>/<k>）；解析失败返回档案引用不支持的同一类错误。
func parseCredentialRef(ref string) (kind, group, key string, err error) {
	kind, rest, ok := strings.Cut(ref, ":")
	if !ok {
		return "", "", "", fmt.Errorf("unsupported credential ref %q", ref)
	}
	group, key, ok = strings.Cut(rest, "/")
	if !ok || group == "" || key == "" {
		return "", "", "", fmt.Errorf("unsupported credential ref %q", ref)
	}
	return kind, group, key, nil
}

// codexEnvPlan 描述 codex 的 env_key 名决议结果（ADR-0024）。
type codexEnvPlan struct {
	// Name 是写进 env_key 的环境变量名。
	Name string
	// SeedRef 非空表示该名字默认不由 vault 提供，需要在默认 env 组补一条值为
	// SeedRef 的引用条目，使 `senv env export` 能提供该名字。
	SeedRef string
}

// codexEnvPlanFor 决定 codex 的 env_key 名：
//   - env:<g>/<k> → <k>：这正是 `senv env export` 已经提供给 shell 的名字，
//     切换无需写任何 vault 条目；
//   - text:<g>/<k>（含 alias 规范引用 text:llm-keys/<alias>）→ alias 派生名，
//     由 ensureCodexEnvName 在默认组补一条指向该条目的引用条目。
func codexEnvPlanFor(entry *storage.LLMProviderEntry, alias string) (codexEnvPlan, error) {
	kind, group, key, err := parseCredentialRef(entry.CredentialRef)
	if err != nil {
		return codexEnvPlan{}, err
	}
	switch kind {
	case "env":
		return codexEnvPlan{Name: key}, nil
	case "text":
		return codexEnvPlan{Name: senvEnvKeyName(alias), SeedRef: "{{text:" + group + ":" + key + "}}"}, nil
	default:
		return codexEnvPlan{}, fmt.Errorf("unsupported credential ref %q", entry.CredentialRef)
	}
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

// atomicWriteWithBackup 将原文件复制为 <path>.senv-bak（供写入中途崩溃后
// 人工恢复），再以 temp+rename 原子替换 path；替换成功后立即删除备份——
// 备份内容含旧凭据，不能在磁盘上无限期残留。原文件不存在时跳过备份。
func atomicWriteWithBackup(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	hadBackup := false
	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
		if err := os.WriteFile(path+".senv-bak", existing, 0o600); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
		hadBackup = true
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
	if hadBackup {
		// best-effort：删除失败只留一个可人工恢复的备份，不值得报错回滚
		os.Remove(path + ".senv-bak")
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

// claudeCodeAdapter：~/.claude/settings.json 顶层 model（默认模型）+
// modelPicker（Agent 模型集）+ env 块（ANTHROPIC_BASE_URL /
// ANTHROPIC_AUTH_TOKEN），凭据内联。provider 是自定义接入地址，
// replaceBuiltInOptions 让内置 lineup 不出现在选择器里（D5）。
const (
	// claudeBehavesAsModel 是 Claude Code 已知的稳定模型，用作自定义模型
	// 的客户端能力映射；behavesAs 只影响客户端行为，不改实际发送的模型 ID。
	claudeBehavesAsModel = "claude-sonnet-4-5"
	claudeContext1M      = 1_000_000
)

func claudeBehavesAs(meta ModelMetadata) string {
	if meta.ContextLimit >= claudeContext1M {
		return claudeBehavesAsModel + "[1m]"
	}
	return claudeBehavesAsModel
}

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
				root["model"] = req.DefaultModel
				env := ensureSubMap(root, "env")
				env["ANTHROPIC_BASE_URL"] = req.BaseURL
				env["ANTHROPIC_AUTH_TOKEN"] = req.Credential
				options := make([]map[string]any, 0, len(req.Models))
				for _, model := range req.Models {
					meta := req.ModelMetadata[model]
					option := map[string]any{
						"model":     model,
						"label":     modelLabel(model, meta),
						"behavesAs": claudeBehavesAs(meta),
					}
					if meta.Description != "" {
						option["description"] = meta.Description
					}
					options = append(options, option)
				}
				root["modelPicker"] = map[string]any{
					"options":               options,
					"replaceBuiltInOptions": true,
				}
				return nil
			}, req.tx)
		},
	}
}

// codexAdapter：~/.codex/config.toml 顶层 model/model_provider/
// model_catalog_json + [model_providers.<senv id>]，并生成
// ~/.codex/model-catalogs/senv-<alias>.json 作为会话内模型选择器的数据源；
// 凭据只写 env_key 名，明文不落盘。catalog 文件是 senv 自有派生文件，由
// SwitchManager 纳入事务并在 alias 失效时清理。
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
		OwnedArtifacts: func(home, alias string) []string {
			return []string{codexCatalogPath(home, alias)}
		},
		Credential: CredentialEnvVar,
		Apply: func(req SwitchRequest) error {
			id := senvProviderID(req.ProviderAlias)
			if strings.TrimSpace(req.Home) == "" {
				return fmt.Errorf("codex switch requires the agent home to write the model catalog")
			}
			data, err := buildCodexCatalog(req)
			if err != nil {
				return err
			}
			if err := writeConfigFile(codexCatalogPath(req.Home, req.ProviderAlias), data, req.tx); err != nil {
				return err
			}
			return applyTOMLMerge(req.ConfigPath, func(root map[string]any) error {
				root["model"] = req.DefaultModel
				root["model_provider"] = id
				root["model_catalog_json"] = codexCatalogRelPath(req.ProviderAlias)
				setTOMLPath(root, []string{"model_providers", id}, map[string]any{
					"name":                 "senv " + req.ProviderAlias,
					"base_url":             req.BaseURL,
					"env_key":              req.Credential,
					"wire_api":             "responses",
					"requires_openai_auth": false,
				})
				if prior := req.PriorProvider; prior != "" && prior != req.ProviderAlias {
					deleteTOMLPath(root, []string{"model_providers", senvProviderID(prior)})
				}
				return nil
			}, req.tx)
		},
	}
}

// kimiAdapter：~/.kimi-code/config.toml（Kimi Code CLI）——default_model
// 顶层键 + [providers."senv-*"] + 每个 Agent 模型集成员一条
// [models."senv-*/<model>"]，api_key 内联。
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
			modelAlias := id + "/" + req.DefaultModel
			return applyTOMLMerge(req.ConfigPath, func(root map[string]any) error {
				root["default_model"] = modelAlias
				setTOMLPath(root, []string{"providers", id}, map[string]any{
					"type":     kimiProviderType(req.APIShape),
					"base_url": req.BaseURL,
					"api_key":  req.Credential,
				})
				models := ensureSubMap(root, "models")
				// 差集清理：换 provider 时清掉旧命名空间，同 provider 缩集时
				// 清掉本次未选中的条目；只匹配 senv-<alias>/ 前缀。
				if prior := req.PriorProvider; prior != "" {
					priorID := senvProviderID(prior)
					if prior != req.ProviderAlias {
						deleteTOMLPath(root, []string{"providers", priorID})
						removeModelEntries(models, priorID, nil)
					} else {
						removeModelEntries(models, id, req.Models)
					}
				}
				for _, model := range req.Models {
					meta := req.ModelMetadata[model]
					entry := map[string]any{
						"provider":         id,
						"model":            model,
						"max_context_size": kimiContextSize(meta),
						"display_name":     modelLabel(model, meta),
					}
					if meta.OutputLimit > 0 {
						entry["max_output_size"] = meta.OutputLimit
					}
					if caps := kimiCapabilities(meta); len(caps) > 0 {
						entry["capabilities"] = caps
					}
					if modelSupportsReasoning(meta) {
						entry["support_efforts"] = append([]string(nil), meta.ReasoningEfforts...)
					}
					setTOMLPath(root, []string{"models", id + "/" + model}, entry)
				}
				return nil
			}, req.tx)
		},
	}
}

// piAPIType 返回 pi provider 的 api 字段：档案显式声明 openai-responses 时
// 走 Responses 线协议，其余（未声明 / openai-chat）保持 Chat Completions。
func piAPIType(declaredShape string) string {
	if declaredShape == storage.LLMAPIShapeOpenAIResponses {
		return "openai-responses"
	}
	return "openai-completions"
}

// kimiProviderType 与 piAPIType 同理：kimi-code 的 provider type 在 Chat
// Completions（openai）与 Responses（openai_responses）之间按声明形态选择。
func kimiProviderType(declaredShape string) string {
	if declaredShape == storage.LLMAPIShapeOpenAIResponses {
		return "openai_responses"
	}
	return "openai"
}

// opencodeProviderNPM 返回 opencode provider 的 npm 适配包：声明
// openai-responses 的档案走 @ai-sdk/openai（默认 Responses 线协议），其余
// 用通用 openai-compatible（Chat Completions）。
func opencodeProviderNPM(declaredShape string) string {
	if declaredShape == storage.LLMAPIShapeOpenAIResponses {
		return "@ai-sdk/openai"
	}
	return "@ai-sdk/openai-compatible"
}

// modelSupportsReasoning 报告目录/档案元数据是否标明该模型具备推理能力。
func modelSupportsReasoning(meta ModelMetadata) bool {
	return len(meta.ReasoningEfforts) > 0
}

// filterModalities 按目标 agent 支持的输入模态白名单收敛模态列表。models.dev
// 目录声明的模态可能包含 audio/pdf/video 等，超出部分 agent 配置的 schema
// 取值范围（pi 仅 text/image，codex 仅 text/image/audio），原样透传会让 agent
// 启动时拒绝整个配置文件。保序去重，大小写不敏感，白名单外的值丢弃。
func filterModalities(mods []string, allowed ...string) []string {
	if len(mods) == 0 {
		return nil
	}
	ok := make(map[string]struct{}, len(allowed))
	for _, a := range allowed {
		ok[strings.ToLower(a)] = struct{}{}
	}
	seen := make(map[string]struct{}, len(mods))
	out := make([]string, 0, len(mods))
	for _, mod := range mods {
		m := strings.ToLower(strings.TrimSpace(mod))
		if _, in := ok[m]; !in {
			continue
		}
		if _, dup := seen[m]; dup {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

// piInputModalities 收敛到 pi models.json schema 允许的 text/image；全被
// 过滤时返回 nil，调用方省略 input 字段（pi 缺省即 text+image）。
func piInputModalities(mods []string) []string {
	return filterModalities(mods, "text", "image")
}

func kimiCapabilities(meta ModelMetadata) []string {
	var caps []string
	if modelSupportsReasoning(meta) {
		caps = append(caps, "thinking")
	}
	for _, mod := range meta.InputModalities {
		switch strings.ToLower(mod) {
		case "image":
			caps = append(caps, "image_in")
		case "video":
			caps = append(caps, "video_in")
		}
	}
	return caps
}

// piAdapter：<PI agent dir>（$PI_CODING_AGENT_DIR，默认 ~/.pi/agent）下
// models.json 写 provider 定义（含 apiKey）、settings.json 写
// defaultProvider/defaultModel。两份文件属于同一事务，第二份失败时第一份由
// SwitchManager 统一回滚。agent dir 与 agentcfg 的 MCP 目标共用一处解析。
func piAdapter() AgentAdapter {
	return AgentAdapter{
		ID:       "pi",
		Name:     "Pi",
		Protocol: ProtocolOpenAICompatible,
		ConfigPath: func(home string) string {
			return filepath.Join(agentcfg.PiAgentDir(home), "models.json")
		},
		ConfigPaths: func(home string) []string {
			return []string{
				filepath.Join(agentcfg.PiAgentDir(home), "models.json"),
				filepath.Join(agentcfg.PiAgentDir(home), "settings.json"),
			}
		},
		Credential: CredentialInline,
		Apply: func(req SwitchRequest) error {
			id := senvProviderID(req.ProviderAlias)
			if err := applyJSONMerge(req.ConfigPath, func(root map[string]any) error {
				providers := ensureSubMap(root, "providers")
				if prior := req.PriorProvider; prior != "" && prior != req.ProviderAlias {
					delete(providers, senvProviderID(prior))
				}
				models := make([]map[string]any, 0, len(req.Models))
				for _, model := range req.Models {
					meta := req.ModelMetadata[model]
					entry := map[string]any{
						"id":   model,
						"name": modelLabel(model, meta),
					}
					// pi 对缺失字段用内置默认（contextWindow 128k、maxTokens
					// 16k、reasoning false），只在已知时写入真实值；写 0 会被
					// pi 直接拒绝。
					if meta.ContextLimit > 0 {
						entry["contextWindow"] = meta.ContextLimit
					}
					if meta.OutputLimit > 0 {
						entry["maxTokens"] = meta.OutputLimit
					}
					if modelSupportsReasoning(meta) {
						entry["reasoning"] = true
					}
					if input := piInputModalities(meta.InputModalities); len(input) > 0 {
						entry["input"] = input
					}
					models = append(models, entry)
				}
				providers[id] = map[string]any{
					"baseUrl": req.BaseURL,
					"api":     piAPIType(req.APIShape),
					"apiKey":  req.Credential,
					// compat 是 provider 级兼容声明，不是模型元数据投影，因此不受
					// 上面「元数据缺失就省略字段」规则的约束。pi 以
					// model.reasoning && compat.supportsDeveloperRole 决定 system
					// prompt 用 developer 还是 system 角色，而 supportsDeveloperRole
					// 的缺省推断只认一份硬编码的 base URL/provider 特征名单：senv 写
					// 入的自建网关（new-api 等）不在名单内，会被当成标准 OpenAI，使
					// 被投影为 reasoning 的模型发出上游不接受的 developer 角色
					// （Moonshot/Kimi 等报 400 role 'developer' is not allowed）。
					// 一律写 false：system 是所有 OpenAI 兼容端点的公共子集，真
					// OpenAI 也接受它；写在 provider 级才能覆盖全部模型（含重跑新增
					// 的），也不必跟着 pi 的名单漂移。注意 senv 整体拥有该 provider
					// 对象，用户手工补的 compat 会被下次切换覆盖，所以开关必须在这里。
					// 不写 supportsReasoningEffort：它是 reasoning_effort 的透传开关，
					// 一刀切关掉会让指向真 OpenAI 的档案失去推理档位。
					"compat": map[string]any{"supportsDeveloperRole": false},
					"models": models,
				}
				return nil
			}, req.tx); err != nil {
				return err
			}
			settings := filepath.Join(filepath.Dir(req.ConfigPath), "settings.json")
			return applyJSONMerge(settings, func(root map[string]any) error {
				root["defaultProvider"] = id
				root["defaultModel"] = req.DefaultModel
				// Pi 启动时，非空 enabledModels 会先生成 scoped model 列表，并优先
				// 选第一个 scoped model；把本次默认模型放在首位，才能压过用户
				// 原有 allowlist 的顺序，同时保留其余用户 scope。
				priorID := ""
				if req.PriorProvider != "" {
					priorID = senvProviderID(req.PriorProvider)
				}
				updatePiEnabledModels(root, id, req.DefaultModel, priorID)
				return nil
			}, req.tx)
		},
	}
}

func updatePiEnabledModels(root map[string]any, providerID, defaultModel, priorProviderID string) {
	raw, ok := root["enabledModels"]
	if !ok {
		return
	}
	patterns, ok := stringValues(raw)
	if !ok || len(patterns) == 0 {
		return
	}
	removeProviders := map[string]struct{}{providerID: {}}
	if priorProviderID != "" {
		removeProviders[priorProviderID] = struct{}{}
	}
	kept := make([]any, 0, len(patterns)+2)
	for _, pattern := range patterns {
		provider, _, ok := strings.Cut(strings.TrimSpace(pattern), "/")
		if _, remove := removeProviders[provider]; ok && remove {
			continue
		}
		kept = append(kept, pattern)
	}
	root["enabledModels"] = append([]any{
		providerID + "/" + defaultModel,
		providerID + "/*",
	}, kept...)
}

func stringValues(value any) ([]string, bool) {
	switch values := value.(type) {
	case []any:
		out := make([]string, 0, len(values))
		for _, item := range values {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, text)
		}
		return out, true
	case []string:
		return append([]string(nil), values...), true
	default:
		return nil, false
	}
}

// opencodeAdapter：~/.config/opencode/opencode.json provider.<senv id>
// （npm openai-compatible + options.baseURL/apiKey + models map，含全部
// Agent 模型集成员）+ 顶层 model = "<id>/<默认模型>"。
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
				if prior := req.PriorProvider; prior != "" && prior != req.ProviderAlias {
					delete(providers, senvProviderID(prior))
				}
				models := make(map[string]any, len(req.Models))
				for _, model := range req.Models {
					meta := req.ModelMetadata[model]
					entry := map[string]any{"name": modelLabel(model, meta)}
					// opencode 对缺省 limit 记 0、reasoning 记 false；已知时
					// 写入真实值，让 agent 侧上下文统计与推理开关保持正确。
					if meta.ContextLimit > 0 || meta.OutputLimit > 0 {
						limit := map[string]any{}
						if meta.ContextLimit > 0 {
							limit["context"] = meta.ContextLimit
						}
						if meta.OutputLimit > 0 {
							limit["output"] = meta.OutputLimit
						}
						entry["limit"] = limit
					}
					if modelSupportsReasoning(meta) {
						entry["reasoning"] = true
					}
					if len(meta.InputModalities) > 0 {
						entry["modalities"] = map[string]any{
							"input": append([]string(nil), meta.InputModalities...),
						}
					}
					models[model] = entry
				}
				providers[id] = map[string]any{
					"npm":  opencodeProviderNPM(req.APIShape),
					"name": "senv " + req.ProviderAlias,
					"options": map[string]any{
						"baseURL": req.BaseURL,
						"apiKey":  req.Credential,
					},
					"models": models,
				}
				root["model"] = id + "/" + req.DefaultModel
				return nil
			}, req.tx)
		},
	}
}

// ---------------------------------------------------------------------------
// 适配器共用的投影与清理原语
// ---------------------------------------------------------------------------

// modelLabel 返回模型在 agent 选择器里的展示名：目录有 name 用 name，否则
// 回退模型 id（目录是可选增强）。
func modelLabel(model string, meta ModelMetadata) string {
	if name := strings.TrimSpace(meta.Name); name != "" {
		return name
	}
	return model
}

// kimiMaxContextSizeFallback 是目录缺 limit.context 时的保守回退值：Kimi
// Code 的 max_context_size 是必填项，宁低估不虚高压缩阈值。
const kimiMaxContextSizeFallback = 131072

// kimiContextSize 取目录里的真实上下文长度，缺失时回退保守值。
func kimiContextSize(meta ModelMetadata) int {
	if meta.ContextLimit > 0 {
		return meta.ContextLimit
	}
	return kimiMaxContextSizeFallback
}

// removeModelEntries 删除 models 表里 providerID 命名空间（`<id>/` 前缀）下
// 不在 keep 中的条目；keep 为 nil 表示整段命名空间都删。用户自有条目不带
// senv 前缀，因此不受影响。
func removeModelEntries(models map[string]any, providerID string, keep []string) {
	prefix := providerID + "/"
	keepKeys := make(map[string]struct{}, len(keep))
	for _, model := range keep {
		keepKeys[prefix+model] = struct{}{}
	}
	for key := range models {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if _, ok := keepKeys[key]; ok {
			continue
		}
		delete(models, key)
	}
}

// deleteTOMLPath 删除一个点分路径；中间表不存在时视为已删除（幂等）。
func deleteTOMLPath(root map[string]any, path []string) {
	current := root
	for _, key := range path[:len(path)-1] {
		next, _ := current[key].(map[string]any)
		if next == nil {
			return
		}
		current = next
	}
	delete(current, path[len(path)-1])
}

// writeConfigFile 写入一个 senv 自有文件：有事务时走事务（失败可回滚），
// 无事务时退化为带备份的原子写（适配器被直接调用时）。
func writeConfigFile(path string, data []byte, tx *configTransaction) error {
	if tx != nil {
		return tx.write(path, data)
	}
	return atomicWriteWithBackup(path, data)
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
// manager。不支持其他前缀（档案写入时已校验，此处兜底）。档案本体跨机同步
// 后（凭据本体不出机），引用指向的条目可能尚未在本机——缺失时错误需指明
// 完整引用名与修复指引，而不是笼统的解密失败。
func resolveCredential(entry *storage.LLMProviderEntry, pm *ProviderManager) (string, error) {
	kind, group, key, err := parseCredentialRef(entry.CredentialRef)
	if err != nil {
		return "", err
	}
	switch kind {
	case "text":
		tm := pm.textManager()
		value, err := tm.Get(group, key)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("凭据引用 %s 在本机 vault 中不存在：先 `senv text add %s %s`（或从已有该凭据的机器同步）再切换",
					entry.CredentialRef, group, key)
			}
			return "", fmt.Errorf("decrypt credential %s: %w", entry.CredentialRef, err)
		}
		return value, nil
	case "env":
		em := pm.envManager()
		value, err := em.Get(group, key)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("凭据引用 %s 在本机 vault 中不存在：先 `senv env set %s %s <value>`（或从已有该凭据的机器同步）再切换",
					entry.CredentialRef, group, key)
			}
			return "", fmt.Errorf("decrypt credential %s: %w", entry.CredentialRef, err)
		}
		return value, nil
	default:
		return "", fmt.Errorf("unsupported credential ref %q", entry.CredentialRef)
	}
}

// SwitchOutput 携带切换结果与给用户的后续提示。
type SwitchOutput struct {
	AgentID      string
	AgentName    string
	Provider     string
	Models       []string
	DefaultModel string
	// BaseURL 是按该 agent 协议族解析后实际写入配置的接入地址。
	BaseURL string
	// BaseURLSource 标注地址来源：显式形态地址字段名，或「由 BaseURL 推断」。
	BaseURLSource string
	ConfigPath    string
	CredentialEnv string // 非空表示凭据需经该环境变量暴露
	Warnings      []string
}

// Switch 执行完整切换：校验 → 解密凭据 → 适配器写回 → 指针更新（失败回滚）。
func (sm *SwitchManager) Switch(agentID, providerAlias string, models []string, defaultModel string) (*SwitchOutput, error) {
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

	// Agent 模型集与默认模型先解析、后校验，任何非法输入都不触碰文件。
	agentModels, err := resolveAgentModels(providerAlias, entry, models)
	if err != nil {
		return nil, err
	}
	defaultModel, err = resolveDefaultModel(providerAlias, entry, agentModels, defaultModel)
	if err != nil {
		return nil, err
	}

	// 形态地址门禁：目标协议族存在显式形态地址时放行——该地址的存在本身就是
	// 「该族被服务」的声明，不受 api_shape 声明影响；否则维持 ADR-0006 的声明
	// 判据，拒绝文案给三个可行动作。声明值同时传给适配器，供 OpenAI 兼容族内
	// 仍支持 chat 线协议的 agent（kimi/pi/opencode）选线协议。
	shape, err := ParseAPIShape(entry.APIShape)
	if err != nil {
		return nil, err
	}
	if !familyHasExplicitShapeURL(entry, adapter.Protocol) && shape != "" {
		family, ok := shape.Protocol()
		if !ok || family != adapter.Protocol {
			return nil, fmt.Errorf(
				"provider %q declares api_shape %s, which is incompatible with agent %s (%s); change the provider api_shape, add a shape URL for the agent's family via --shape-url <api_shape>=<url>, or switch to a different provider",
				providerAlias, shape, adapter.Name, DescribeProtocol(adapter.Protocol))
		}
	}
	// codex 上游已移除 chat 线协议（openai/codex#7782），只讲 Responses：声明
	// openai-chat 的档案若没有 responses 接入地址，写出的配置必然不可用，
	// fail-closed 拒绝而非写坏。已有 responses_base_url 的档案视为同时服务
	// Responses 形态（ADR-0027），照常放行。
	if adapter.ID == "codex" && shape == APIShapeOpenAIChat && entry.ResponsesBaseURL == "" {
		return nil, fmt.Errorf(
			"provider %q declares api_shape openai-chat, which codex can no longer use (chat wire protocol removed upstream); add a responses_base_url via --shape-url openai-responses=<url>, change the provider api_shape, or switch to a different provider",
			providerAlias)
	}

	// 凭据决议先于任何写回：Inline 族取明文，EnvVar 族（codex）取 env_key 名并
	// 保证该名字可由 `senv env export` 提供（ADR-0024）。两族都必须解密凭据以
	// 校验条目在本机存在（ADR-0019 fail-closed），EnvVar 族不落任何明文。
	var credentialWarnings []string
	credential := ""
	if adapter.Credential == CredentialInline {
		if credential, err = resolveCredential(entry, sm.providerManager); err != nil {
			return nil, err
		}
	} else {
		plain, err := resolveCredential(entry, sm.providerManager)
		if err != nil {
			return nil, err
		}
		plan, err := codexEnvPlanFor(entry, providerAlias)
		if err != nil {
			return nil, err
		}
		if credentialWarnings, err = sm.ensureCodexEnvName(plan, plain); err != nil {
			return nil, err
		}
		credential = plan.Name
	}

	// 地址解析：目标族显式形态地址优先——anthropic_base_url 原样写回（不剥
	// 版本段），OpenAI 族按已解析线协议取 chat_base_url / responses_base_url；
	// 未设回落 BaseURL 按协议族转换（Anthropic 族剥离末段 /v1）。归一幂等，
	// 存量档案无需迁移；来源随输出可见。
	var explicitURL, explicitField string
	switch {
	case adapter.Protocol == ProtocolAnthropic:
		explicitURL, explicitField = entry.AnthropicBaseURL, "anthropic_base_url"
	case agentPrefersChatWire(agentID, shape):
		explicitURL, explicitField = entry.ChatBaseURL, "chat_base_url"
	default:
		explicitURL, explicitField = entry.ResponsesBaseURL, "responses_base_url"
	}
	var baseURL, baseURLSource string
	if explicitURL != "" {
		baseURL, baseURLSource = explicitURL, explicitField
	} else {
		baseURL, baseURLSource = baseURLForFamily(entry.BaseURL, adapter.Protocol), baseURLSourceInferred
	}

	// 指针先读后写：既取上一次指向作为清理依据，也保证损坏的指针文件在写
	// 配置之前就暴露出来。
	pf, err := LoadPointers(pointerPath)
	if err != nil {
		if errors.Is(err, ErrPointerNotFound) {
			pf = &PointerFile{}
		} else {
			return nil, fmt.Errorf("load agent pointers: %w", err)
		}
	}
	prior, _ := pf.Get(agentID)

	configPath := adapter.ConfigPath(home)
	txPaths := []string{configPath}
	if adapter.ConfigPaths != nil {
		txPaths = adapter.ConfigPaths(home)
	}
	// 派生自有文件（codex catalog）纳入同一事务：本次要写的 alias 与指针里
	// 出现过的所有 alias 都要快照，缩集/换 provider 后的清理才能回滚。
	if adapter.OwnedArtifacts != nil {
		for alias := range pointerAliases(pf, providerAlias) {
			txPaths = append(txPaths, adapter.OwnedArtifacts(home, alias)...)
		}
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
		Models:        agentModels,
		DefaultModel:  defaultModel,
		Credential:    credential,
		ConfigPath:    configPath,
		Home:          home,
		ModelMetadata: ResolveModelMetadata(entry.ModelInfo, DefaultModelCatalogPath(pointerPath), entry.CatalogProvider, agentModels),
		APIShape:      string(shape),
		PriorProvider: prior.Provider,
		PriorModels:   prior.Models,
		tx:            tx,
	}
	if err := adapter.Apply(req); err != nil {
		return nil, fmt.Errorf("write %s config: %w", agentID, err)
	}

	// 清理不再被任何 agent 指针指向的 provider 派生文件（如失效的 codex
	// catalog）。删除先于指针落盘，失败走同一个回滚。
	if adapter.OwnedArtifacts != nil {
		for _, alias := range stalePointerAliases(pf, agentID, providerAlias) {
			for _, path := range adapter.OwnedArtifacts(home, alias) {
				if err := tx.remove(path); err != nil {
					return nil, fmt.Errorf("remove stale %s artifact: %w", agentID, err)
				}
			}
		}
	}

	pf.Set(agentID, providerAlias, agentModels, defaultModel)
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
		AgentID:       agentID,
		AgentName:     adapter.Name,
		Provider:      providerAlias,
		Models:        agentModels,
		DefaultModel:  defaultModel,
		BaseURL:       baseURL,
		BaseURLSource: baseURLSource,
		ConfigPath:    configPath,
	}
	if adapter.Credential == CredentialEnvVar {
		out.CredentialEnv = credential
	}
	out.Warnings = append(credentialWarnings, metadataDeclarationWarnings(agentModels, req.ModelMetadata)...)
	return out, nil
}

func metadataDeclarationWarnings(models []string, meta map[string]ModelMetadata) []string {
	var missingDefault []string
	for _, id := range models {
		m := meta[id]
		if len(m.ReasoningEfforts) > 0 && strings.TrimSpace(m.DefaultReasoning) == "" {
			missingDefault = append(missingDefault, id)
		}
	}
	if len(missingDefault) == 0 {
		return nil
	}
	return []string{fmt.Sprintf(
		"模型 %s 未声明默认推理档，切换已使用 agent 模板；可用 senv ai provider edit 补全",
		strings.Join(missingDefault, ", "))}
}

// StatusRow 是 status 输出的一行。
type StatusRow struct {
	AgentID    string
	AgentName  string
	Pointer    *AgentPointer
	ConfigPath string
	// Drift 非空表示指针里的 Agent 模型集已与档案不一致（档案缩集或改名）；
	// 档案不可得（未解锁、档案被删）时为空，status 不因此报错。
	Drift string
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
		row := StatusRow{AgentID: a.ID, AgentName: a.Name, ConfigPath: a.ConfigPath(home)}
		if p, ok := pf.Get(a.ID); ok {
			p := p
			row.Pointer = &p
			row.Drift = sm.driftDetail(p)
		}
		rows = append(rows, row)
	}
	return rows, warning, nil
}

// driftDetail 判定漂移：指针记录的模型已不在档案当前模型集中（档案缩集或
// 改名）时给出可操作提示。指针是子集（用户显式缩小过模型集）不算漂移。
// 不读 agent 配置文件；档案不可得时返回空串，绝不因此让 status 失败。
func (sm *SwitchManager) driftDetail(p AgentPointer) string {
	if sm.providerManager == nil || p.Provider == "" || len(p.Models) == 0 {
		return ""
	}
	entry, err := sm.providerManager.GetProvider(p.Provider)
	if err != nil {
		return ""
	}
	var missing []string
	for _, model := range p.Models {
		if !slices.Contains(entry.Models, model) {
			missing = append(missing, model)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("档案 %s 已不含模型 %s，重新执行 senv ai switch 可对齐",
		p.Provider, strings.Join(missing, ", "))
}

// supportedAgentIDs 返回全部受支持 agent 的 id（报错信息用）。
func supportedAgentIDs() []string {
	var ids []string
	for _, a := range SupportedAgents() {
		ids = append(ids, a.ID)
	}
	return ids
}

// pointerAliases 返回指针文件里出现过的 provider alias 与本次 alias 的并集，
// 用于把可能被清理的派生文件全部纳入事务快照。
func pointerAliases(pf *PointerFile, providerAlias string) map[string]struct{} {
	aliases := map[string]struct{}{providerAlias: {}}
	if pf == nil {
		return aliases
	}
	for _, p := range pf.Agents {
		if p.Provider != "" {
			aliases[p.Provider] = struct{}{}
		}
	}
	return aliases
}

// stalePointerAliases 返回本次切换完成后不再被任何 agent 指向的 provider
// alias（升序）：senv 自有派生文件按这些 alias 清理。
func stalePointerAliases(pf *PointerFile, agentID, providerAlias string) []string {
	live := map[string]struct{}{providerAlias: {}}
	before := map[string]struct{}{}
	if pf != nil {
		for id, p := range pf.Agents {
			if p.Provider == "" {
				continue
			}
			before[p.Provider] = struct{}{}
			if id != agentID {
				live[p.Provider] = struct{}{}
			}
		}
	}
	var stale []string
	for alias := range before {
		if _, ok := live[alias]; ok {
			continue
		}
		stale = append(stale, alias)
	}
	sort.Strings(stale)
	return stale
}

// ensureCodexEnvName 保证 codex 的 env_key 名可由 `senv env export` 提供，并把
// 需要用户动作的情形作为 warning 返回（ADR-0024）。名字无法落地时返回错误，
// 调用方据此保持「零配置写入」。
func (sm *SwitchManager) ensureCodexEnvName(plan codexEnvPlan, credential string) ([]string, error) {
	envMgr := sm.providerManager.envManager()
	settings, err := sm.providerManager.storage.LoadSettings()
	if err != nil {
		return nil, fmt.Errorf("load env settings for codex credential %s: %w", plan.Name, err)
	}
	defaultGroup := settings.DefaultGroup
	if defaultGroup == "" {
		defaultGroup = storage.ConfigDefaultGroup
	}
	// Snapshot 一次取全部分组与变量：GroupInfo.IsActive 已含默认组，等价于
	// `senv env export` 的导出集合（internal/env 的 Export 语义）。
	vars, groups, err := envMgr.Snapshot()
	if err != nil {
		return nil, fmt.Errorf("read env groups for codex credential %s: %w", plan.Name, err)
	}
	var (
		exportedValue string
		exported      bool
		inactive      []string
	)
	for _, g := range groups {
		value, ok := vars[g.Name][plan.Name]
		if !ok {
			continue
		}
		if g.IsActive {
			exportedValue, exported = value, true
			continue
		}
		inactive = append(inactive, g.Name)
	}

	switch {
	case exported:
		// 名字已由导出集合里的条目提供：不写 vault；只有该条目的解析结果不是
		// 本次凭据时才提示，避免静默取到别的凭据。
		if plan.SeedRef == "" || envValueMatchesCredential(exportedValue, credential, sm.providerManager) {
			return nil, nil
		}
		return []string{fmt.Sprintf(
			"环境变量 %s 已存在且不指向本次凭据：codex 会取到该条目当前的值，认证可能失败；如需改用本次凭据请核对 `senv env set %s <value>`",
			plan.Name, plan.Name)}, nil
	case len(inactive) > 0:
		// 名字只存在于未激活组：导出不会包含它，补写默认组会与用户既有条目
		// 重复，因此只提示激活。
		return []string{fmt.Sprintf(
			"环境变量 %s 所在组 %s 未激活：`senv env export` 不会提供该名字，codex 会报 Missing environment variable；请执行 `senv env group activate %s`",
			plan.Name, strings.Join(inactive, "、"), inactive[0])}, nil
	case plan.SeedRef == "":
		return nil, fmt.Errorf(
			"codex credential env %s is not provided by any env group; set it with `senv env set %s <value>`", plan.Name, plan.Name)
	default:
		if err := envMgr.Set(defaultGroup, plan.Name, plan.SeedRef); err != nil {
			return nil, fmt.Errorf("write codex credential env %s to group %s: %w", plan.Name, defaultGroup, err)
		}
		return []string{fmt.Sprintf(
			"已在组 %s 写入环境变量 %s = %s：`senv env export` 会把它解析为本次凭据",
			defaultGroup, plan.Name, plan.SeedRef)}, nil
	}
}

// envValueMatchesCredential 用既有引用解析器判断既有 env 条目是否解析为本次
// 凭据明文（宽松模式：未知引用保留字面量，按不相等处理）。
func envValueMatchesCredential(value, credential string, pm *ProviderManager) bool {
	if value == credential {
		return true
	}
	if !ref.HasReferences(value) {
		return false
	}
	resolved, err := ref.Resolve(value, providerRefGetter{env: pm.envManager(), text: pm.textManager()}, ref.ResolveOptions{Loose: true})
	if err != nil {
		return false
	}
	return resolved == credential
}

// providerRefGetter 是 internal/ref 的 ValueGetter 在 LLM 包内的最小实现。
type providerRefGetter struct {
	env  *env.Manager
	text *text.Manager
}

func (g providerRefGetter) GetEnvValue(group, key string) (string, error) {
	return g.env.Get(group, key)
}

func (g providerRefGetter) GetTextValue(group, key string) (string, error) {
	return g.text.Get(group, key)
}

func sortedContains(sorted []string, want string) bool {
	for _, v := range sorted {
		if v == want {
			return true
		}
	}
	return false
}

// resolveAgentModels 解析本次写入的 Agent 模型集：未显式给出（nil 或空）
// 时取 Provider 模型集全集；显式给出时保序去重，且每个模型都必须属于档案。
func resolveAgentModels(providerAlias string, entry *storage.LLMProviderEntry, models []string) ([]string, error) {
	if len(models) == 0 {
		if len(entry.Models) == 0 {
			return nil, fmt.Errorf("provider %q has no models", providerAlias)
		}
		return append([]string(nil), entry.Models...), nil
	}
	out := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		if !slices.Contains(entry.Models, model) {
			return nil, fmt.Errorf("model %q is not in provider %q; available: %s",
				model, providerAlias, strings.Join(entry.Models, ", "))
		}
		if _, dup := seen[model]; dup {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("agent model set is empty")
	}
	return out, nil
}

// resolveDefaultModel 解析默认模型：显式值优先，其次档案默认模型，最后在
// Agent 模型集恰有一个时取它；都不成立时报错，MUST NOT 静默取首项。
func resolveDefaultModel(providerAlias string, entry *storage.LLMProviderEntry, models []string, explicit string) (string, error) {
	candidate := explicit
	if candidate == "" {
		candidate = entry.DefaultModel
	}
	if candidate == "" {
		if len(models) == 1 {
			return models[0], nil
		}
		return "", fmt.Errorf("provider %q has no default model; specify the default model explicitly (available: %s)",
			providerAlias, strings.Join(models, ", "))
	}
	if !slices.Contains(models, candidate) {
		return "", fmt.Errorf("default model %q is not in the selected model set (available: %s)",
			candidate, strings.Join(models, ", "))
	}
	return candidate, nil
}
