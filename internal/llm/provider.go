// Provider 档案管理：档案与凭据分离存储（driver 决策 D4）。档案是
// llm_providers/ 下的类型化加密条目；凭据存 vault text 保留组 llm-keys，
// 档案只存引用，因此 list/show/MCP 可以安全触达档案元数据。
package llm

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/text"
)

// LLMKeysGroup 是凭据专用 text 保留组；档案引用其中的条目。
const LLMKeysGroup = "llm-keys"

// catalogStaleAfter 超过该时长的目录缓存视为过期，仅提示不阻断。
const catalogStaleAfter = 7 * 24 * time.Hour

// OwnedCredentialRef 返回 alias 在保留组中的规范凭据引用。
func OwnedCredentialRef(alias string) string {
	return "text:" + LLMKeysGroup + "/" + alias
}

// Manager provides LLM provider profile operations above the encrypted
// storage layer, mirroring the SSH asset manager.
type ProviderManager struct {
	storage        *storage.Manager
	key            []byte
	password       string
	mutationLocked bool
	// removeHooks are package-private seams for compensation tests; production
	// managers leave both nil.
	removeCredential func() error
	removeProfile    func() error
}

func NewProviderManager(store *storage.Manager, password string) *ProviderManager {
	return &ProviderManager{storage: store, password: password}
}

func NewProviderManagerWithKey(store *storage.Manager, key []byte) *ProviderManager {
	return &ProviderManager{storage: store, key: key}
}

func (m *ProviderManager) mutate(fn func(*ProviderManager) error) error {
	if m.mutationLocked {
		return fn(m)
	}
	return m.storage.WithVaultMutation(func(locked *storage.Manager) error {
		clone := *m
		clone.storage = locked
		clone.mutationLocked = true
		if err := clone.ensureKey(); err != nil {
			return err
		}
		return fn(&clone)
	})
}

func (m *ProviderManager) ensureKey() error {
	if m.key != nil {
		return nil
	}
	key, err := m.storage.DeriveKeyFromPassword(m.password)
	if err != nil {
		return err
	}
	m.key = key
	return nil
}

func (m *ProviderManager) textManager() *text.Manager {
	if err := m.ensureKey(); err == nil && m.key != nil {
		return text.NewManagerWithKey(m.storage, m.key)
	}
	return text.NewManager(m.storage, m.password)
}

// envManager 返回 env 凭据读取器（env: 引用解密用）。
func (m *ProviderManager) envManager() *env.Manager {
	if err := m.ensureKey(); err == nil && m.key != nil {
		return env.NewManagerWithKey(m.storage, m.key)
	}
	return env.NewManager(m.storage, m.password)
}

func (m *ProviderManager) ensureLLMKeysGroup() error {
	if err := m.ensureKey(); err != nil {
		return err
	}
	return m.textManager().EnsureGroup(LLMKeysGroup, "reserved: LLM API keys (CLI/TUI only)")
}

func (m *ProviderManager) save(alias string, entry *storage.LLMProviderEntry) error {
	if m.key != nil {
		return m.storage.SaveLLMProviderWithKey(alias, entry, m.key)
	}
	return m.storage.SaveLLMProvider(alias, entry, m.password)
}

func (m *ProviderManager) load(alias string) (*storage.LLMProviderEntry, error) {
	if m.key != nil {
		return m.storage.LoadLLMProviderWithKey(alias, m.key)
	}
	return m.storage.LoadLLMProvider(alias, m.password)
}

// AddProviderOptions 描述一次档案写入的全部输入。
type AddProviderOptions struct {
	Alias           string
	BaseURL         string
	AllowHTTP       bool
	APIKey          string // 与 KeyRef 二选一；写入 llm-keys 组
	KeyRef          string // 形如 env:<g>/<k> 或 text:<g>/<k>
	CatalogPath     string // 模型目录缓存路径
	CatalogProvider string // models.dev provider id
	Models          []string
	// ModelContexts 是调用方显式提供的模型上下文窗口（token 数），覆盖目录值。
	ModelContexts map[string]int
	// ModelOutputs 是调用方显式提供的模型输出上限（token 数），覆盖目录值。
	ModelOutputs map[string]int
	// ModelReasoning 是调用方显式提供的模型推理档位，覆盖目录值；非空即视为
	// 该模型具备推理能力，切换投影按此写各 agent 的推理开关。
	ModelReasoning map[string][]string
	// ModelDefaultReasoning 是调用方显式提供的 per-model 默认推理档，覆盖档案
	// 已有值、集合级声明与目录。
	ModelDefaultReasoning map[string]string
	// DefaultReasoning 是集合级默认推理档，只填充「有档位且尚未解析出默认档」
	// 的模型，不盖到无档位模型上。
	DefaultReasoning string
	// ModelModalities 是调用方显式提供的输入模态，覆盖档案已有值与目录。
	ModelModalities map[string][]string
	// BaseMetadata 是编辑时用于保留既有元数据的内部输入；CLI/TUI 不直接设置。
	BaseMetadata map[string]storage.LLMModelInfo
	// RequireModelMetadata 为 true 时，最终模型集中每个模型都必须解析出
	// ContextWindow；有推理档位的模型还必须解析出默认推理档。旧档案读取/
	// 不影响模型集的编辑保持 false。
	RequireModelMetadata bool
	DefaultModel         string
	// APIShape 可选声明接口形态（openai-chat | openai-responses | anthropic）；
	// 空值表示不声明，切换时按目标 agent 协议族归一（ADR-0006）。
	APIShape string
	// Description is an optional vault note on the provider profile itself,
	// independent of model catalog text in ModelInfo.
	Description string
	Force       bool
}

// AddProviderResult 携带保存结果与非致命警告（如目录缓存过期）。
type AddProviderResult struct {
	Entry    *storage.LLMProviderEntry
	Warnings []string
}

// AddProvider 校验并保存档案。凭据与档案在同一 vault mutation 内写入；
// 档案写入失败时尽力回删本次新建的凭据，避免留下孤儿凭据。
func (m *ProviderManager) AddProvider(opts AddProviderOptions) (*AddProviderResult, error) {
	alias := strings.TrimSpace(opts.Alias)
	if err := storage.ValidateName(alias); err != nil {
		return nil, fmt.Errorf("invalid provider alias %q: %w", opts.Alias, err)
	}
	// 接入地址统一按 OpenAI 兼容形态落库（补末段 /v1、收敛尾斜杠）；切换时
	// 再按 agent 协议族转换。归一不改写校验语义：userinfo、空 host 与非允许
	// 的 HTTP 仍由 ValidateLLMProviderURL 拒绝。
	rawBaseURL := strings.TrimSpace(opts.BaseURL)
	baseURL := baseURLForFamily(rawBaseURL, ProtocolOpenAICompatible)
	if err := storage.ValidateLLMProviderURL(baseURL, opts.AllowHTTP); err != nil {
		return nil, fmt.Errorf("invalid base URL %q: %w", opts.BaseURL, err)
	}
	if err := storage.ValidateLLMProviderAPIShape(strings.TrimSpace(opts.APIShape)); err != nil {
		return nil, err
	}
	desc, err := storage.ValidateDescription(opts.Description, true)
	if err != nil {
		return nil, err
	}
	if err := validateCredentialInput(opts); err != nil {
		return nil, err
	}
	if existing, loadErr := m.load(alias); loadErr == nil {
		opts.BaseMetadata = existing.ModelInfo
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return nil, loadErr
	}
	models, modelInfo, warnings, err := m.assembleModels(opts)
	if err != nil {
		return nil, err
	}
	defaultModel := strings.TrimSpace(opts.DefaultModel)
	if defaultModel != "" && !slices.Contains(models, defaultModel) {
		return nil, fmt.Errorf("default model %q is not in the model set", defaultModel)
	}

	now := time.Now().Truncate(time.Second).UTC()
	entry := &storage.LLMProviderEntry{
		Alias:           alias,
		BaseURL:         baseURL,
		CredentialRef:   opts.KeyRef,
		CatalogProvider: strings.TrimSpace(opts.CatalogProvider),
		APIShape:        strings.TrimSpace(opts.APIShape),
		Models:          models,
		ModelInfo:       modelInfo,
		DefaultModel:    defaultModel,
		Description:     desc,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if opts.APIKey != "" {
		entry.CredentialRef = OwnedCredentialRef(alias)
	}
	err = m.mutate(func(locked *ProviderManager) error {
		existing, loadErr := locked.load(alias)
		if loadErr != nil && !errors.Is(loadErr, os.ErrNotExist) {
			return loadErr
		}
		if existing != nil {
			if !opts.Force {
				return fmt.Errorf("provider %q already exists; use --force to overwrite", alias)
			}
			entry.CreatedAt = existing.CreatedAt
			if opts.Description == "" {
				entry.Description = existing.Description
			}
		}
		if existing == nil && opts.APIKey == "" && opts.KeyRef == "" {
			return fmt.Errorf("either the interactive credential prompt, --api-key-stdin, or --key-ref is required")
		}
		if existing != nil && opts.APIKey == "" && opts.KeyRef == "" {
			// A forced metadata-only update must never destroy the old owned
			// credential or replace its reference.
			entry.CredentialRef = existing.CredentialRef
		}

		oldOwned := existing != nil && existing.CredentialRef == OwnedCredentialRef(alias)
		newOwned := entry.CredentialRef == OwnedCredentialRef(alias)

		if newOwned {
			var oldValue string
			hadOld := false
			if oldOwned {
				value, getErr := locked.textManager().Get(LLMKeysGroup, alias)
				if getErr != nil && !errors.Is(getErr, os.ErrNotExist) {
					return fmt.Errorf("read old credential: %w", getErr)
				}
				oldValue, hadOld = value, getErr == nil
			}
			if opts.APIKey != "" {
				if err := locked.ensureLLMKeysGroup(); err != nil {
					return err
				}
				if err := locked.textManager().Set(LLMKeysGroup, alias, opts.APIKey); err != nil {
					return fmt.Errorf("store credential: %w", err)
				}
			}
			if err := locked.save(alias, entry); err != nil {
				// Vault mutation is a lock, not a cross-collection transaction,
				// so restore the exact prior credential before returning.
				var restoreErr error
				if hadOld {
					restoreErr = locked.textManager().Set(LLMKeysGroup, alias, oldValue)
				} else {
					restoreErr = locked.textManager().Delete(LLMKeysGroup, alias)
				}
				if restoreErr != nil {
					return fmt.Errorf("save provider: %v; restore credential also failed: %w", err, restoreErr)
				}
				return fmt.Errorf("save provider: %w", err)
			}
			return nil
		}

		// New external profiles need no credential mutation. Save first so a
		// storage failure cannot affect credential state.
		if err := locked.save(alias, entry); err != nil {
			return fmt.Errorf("save provider: %w", err)
		}
		if oldOwned {
			if err := locked.textManager().Delete(LLMKeysGroup, alias); err != nil && !errors.Is(err, os.ErrNotExist) {
				// Keep the old profile until cleanup is known to succeed.
				if restoreErr := locked.save(alias, existing); restoreErr != nil {
					return fmt.Errorf("delete old credential: %v; restore old provider also failed: %w", err, restoreErr)
				}
				return fmt.Errorf("delete old credential after provider update: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if baseURL != rawBaseURL {
		warnings = append([]string{fmt.Sprintf("base URL 已规范为 %s", baseURL)}, warnings...)
	}
	return &AddProviderResult{Entry: entry, Warnings: warnings}, nil
}

// EditProviderOptions 描述一次档案编辑。指针字段区分「未提供」（nil，保留
// 原值）与「显式清空」（指向空字符串）；alias 是主键，不在可改字段之列。
type EditProviderOptions struct {
	Alias       string
	BaseURL     *string
	AllowHTTP   bool
	APIKey      string  // 新自有凭据（轮换）；与 KeyRef 互斥
	KeyRef      *string // 外部引用；指向空字符串表示不改变
	CatalogPath string
	// CatalogProvider 非 nil 且非空时，模型集从该目录 provider 重新装配；
	// nil 表示保留原目录来源。
	CatalogProvider *string
	// Models 非 nil 时替换模型集（与 CatalogProvider 一起装配）；nil 表示保留。
	Models []string
	// ModelContexts 非 nil 时补充或覆盖模型上下文窗口；只改元数据时 Models
	// 保持 nil，最终模型集沿用档案原值。空非 nil map 表示清空该维度的全部
	// 既有元数据（nil 才是「未提供」）。
	ModelContexts map[string]int
	// ModelOutputs / ModelReasoning / ModelDefaultReasoning / ModelModalities
	// 与 ModelContexts 同义：显式提供时覆盖对应模型的输出上限 / 推理档位 /
	// 默认推理档 / 输入模态；空非 nil map 清空该维度；nil 表示保留原值。
	ModelOutputs          map[string]int
	ModelReasoning        map[string][]string
	ModelDefaultReasoning map[string]string
	// DefaultReasoning 非 nil 时应用集合级默认推理档（空字符串表示不填充）。
	// 与 ModelDefaultReasoning 空非 nil 组合时，先清空档案既有默认档再按
	// 「集合级 > 目录」重新填充。
	DefaultReasoning *string
	ModelModalities  map[string][]string
	// RequireModelMetadata 与 AddProviderOptions 同义；仅在本次会改动模型集
	// 或元数据时为 true，避免 editor 因旧档案缺元数据而无法修改其他字段。
	RequireModelMetadata bool
	DefaultModel         *string
	APIShape             *string
	// Description nil keeps the current profile note; non-nil replaces it
	// (empty string clears).
	Description *string
}

// EditProvider 更新既有档案。alias 不可改；未提供的字段保持原值。凭据轮换
// 语义与 AddProvider 一致：新自有凭据覆盖旧值、改外部引用删除原自有凭据、
// 未提供凭据来源则保留。任一步失败都不留下部分更新。
func (m *ProviderManager) EditProvider(opts EditProviderOptions) (*AddProviderResult, error) {
	alias := strings.TrimSpace(opts.Alias)
	if err := storage.ValidateName(alias); err != nil {
		return nil, fmt.Errorf("invalid provider alias %q: %w", opts.Alias, err)
	}
	apiKey := strings.TrimSpace(opts.APIKey)
	hasKeyRef := opts.KeyRef != nil && strings.TrimSpace(*opts.KeyRef) != ""
	if apiKey != "" && hasKeyRef {
		return nil, fmt.Errorf("--api-key and --key-ref are mutually exclusive")
	}
	if opts.KeyRef != nil && strings.TrimSpace(*opts.KeyRef) != "" {
		if err := ValidateCredentialRef(*opts.KeyRef); err != nil {
			return nil, err
		}
	}

	existing, err := m.GetProvider(alias)
	if err != nil {
		return nil, err
	}
	entry := *existing
	var warnings []string

	if opts.BaseURL != nil {
		rawBaseURL := strings.TrimSpace(*opts.BaseURL)
		baseURL := baseURLForFamily(rawBaseURL, ProtocolOpenAICompatible)
		if err := storage.ValidateLLMProviderURL(baseURL, opts.AllowHTTP); err != nil {
			return nil, fmt.Errorf("invalid base URL %q: %w", *opts.BaseURL, err)
		}
		if baseURL != rawBaseURL {
			warnings = append(warnings, fmt.Sprintf("base URL 已规范为 %s", baseURL))
		}
		entry.BaseURL = baseURL
	}
	if opts.APIShape != nil {
		shape := strings.TrimSpace(*opts.APIShape)
		if err := storage.ValidateLLMProviderAPIShape(shape); err != nil {
			return nil, err
		}
		entry.APIShape = shape
	}
	if opts.CatalogProvider != nil {
		entry.CatalogProvider = strings.TrimSpace(*opts.CatalogProvider)
	}
	if opts.Models != nil || opts.CatalogProvider != nil || opts.ModelContexts != nil ||
		opts.ModelOutputs != nil || opts.ModelReasoning != nil ||
		opts.ModelDefaultReasoning != nil || opts.DefaultReasoning != nil ||
		opts.ModelModalities != nil {
		models := opts.Models
		if models == nil && opts.CatalogProvider == nil {
			models = existing.Models
		}
		addOpts := AddProviderOptions{
			CatalogPath:           opts.CatalogPath,
			CatalogProvider:       entry.CatalogProvider,
			Models:                models,
			ModelContexts:         opts.ModelContexts,
			ModelOutputs:          opts.ModelOutputs,
			ModelReasoning:        opts.ModelReasoning,
			ModelDefaultReasoning: opts.ModelDefaultReasoning,
			ModelModalities:       opts.ModelModalities,
			BaseMetadata:          existing.ModelInfo,
			RequireModelMetadata:  opts.RequireModelMetadata,
		}
		if opts.DefaultReasoning != nil {
			addOpts.DefaultReasoning = strings.TrimSpace(*opts.DefaultReasoning)
		}
		finalModels, modelInfo, modelWarnings, err := m.assembleModels(addOpts)
		if err != nil {
			return nil, err
		}
		entry.Models = finalModels
		entry.ModelInfo = modelInfo
		warnings = append(warnings, modelWarnings...)
	}
	if opts.DefaultModel != nil {
		entry.DefaultModel = strings.TrimSpace(*opts.DefaultModel)
	}
	if entry.DefaultModel != "" && !slices.Contains(entry.Models, entry.DefaultModel) {
		return nil, fmt.Errorf("default model %q is not in the final model set", entry.DefaultModel)
	}

	if apiKey != "" {
		entry.CredentialRef = OwnedCredentialRef(alias)
	} else if hasKeyRef {
		entry.CredentialRef = strings.TrimSpace(*opts.KeyRef)
	}
	if opts.Description != nil {
		desc, descErr := storage.ValidateDescription(*opts.Description, true)
		if descErr != nil {
			return nil, descErr
		}
		entry.Description = desc
	}
	if err := entry.ValidateLLMProvider(); err != nil {
		return nil, err
	}
	entry.UpdatedAt = time.Now().Truncate(time.Second).UTC()
	updated := entry

	err = m.mutate(func(locked *ProviderManager) error {
		current, loadErr := locked.load(alias)
		if errors.Is(loadErr, os.ErrNotExist) {
			return fmt.Errorf("provider %q not found", alias)
		}
		if loadErr != nil {
			return loadErr
		}
		updated.CreatedAt = current.CreatedAt
		oldOwned := current.CredentialRef == OwnedCredentialRef(alias)
		newOwned := updated.CredentialRef == OwnedCredentialRef(alias)

		if newOwned {
			var oldValue string
			hadOld := false
			if oldOwned {
				value, getErr := locked.textManager().Get(LLMKeysGroup, alias)
				if getErr != nil && !errors.Is(getErr, os.ErrNotExist) {
					return fmt.Errorf("read old credential: %w", getErr)
				}
				oldValue, hadOld = value, getErr == nil
			}
			if apiKey != "" {
				if err := locked.ensureLLMKeysGroup(); err != nil {
					return err
				}
				if err := locked.textManager().Set(LLMKeysGroup, alias, apiKey); err != nil {
					return fmt.Errorf("store credential: %w", err)
				}
			}
			if err := locked.save(alias, &updated); err != nil {
				var restoreErr error
				if hadOld {
					restoreErr = locked.textManager().Set(LLMKeysGroup, alias, oldValue)
				} else {
					restoreErr = locked.textManager().Delete(LLMKeysGroup, alias)
				}
				if restoreErr != nil {
					return fmt.Errorf("save provider: %v; restore credential also failed: %w", err, restoreErr)
				}
				return fmt.Errorf("save provider: %w", err)
			}
			return nil
		}

		// 新档案改走外部引用：先保存，再清理原自有凭据；清理失败回滚档案。
		if err := locked.save(alias, &updated); err != nil {
			return fmt.Errorf("save provider: %w", err)
		}
		if oldOwned {
			if err := locked.textManager().Delete(LLMKeysGroup, alias); err != nil && !errors.Is(err, os.ErrNotExist) {
				if restoreErr := locked.save(alias, current); restoreErr != nil {
					return fmt.Errorf("delete old credential: %v; restore old provider also failed: %w", err, restoreErr)
				}
				return fmt.Errorf("delete old credential after provider update: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// entry 是更新前快照上的副本；重新读回保证返回值与落盘一致。
	saved, err := m.GetProvider(alias)
	if err != nil {
		return nil, err
	}
	return &AddProviderResult{Entry: saved, Warnings: warnings}, nil
}

// validateCredentialInput 校验自有凭据 / --key-ref 恰选其一；force 允许两者
// 都缺失以保留既有引用。
func validateCredentialInput(opts AddProviderOptions) error {
	hasKey := strings.TrimSpace(opts.APIKey) != ""
	hasRef := strings.TrimSpace(opts.KeyRef) != ""
	switch {
	case hasKey && hasRef:
		return fmt.Errorf("--api-key and --key-ref are mutually exclusive")
	case hasKey:
		return nil
	case hasRef:
		return ValidateCredentialRef(opts.KeyRef)
	case opts.Force:
		return nil
	default:
		return fmt.Errorf("either the interactive credential prompt, --api-key-stdin, or --key-ref is required")
	}
}

// ValidateCredentialRef 校验档案凭据引用语法：env:<g>/<k> 或 text:<g>/<k>。
func ValidateCredentialRef(ref string) error {
	kind, rest, found := strings.Cut(ref, ":")
	if !found || (kind != "env" && kind != "text") {
		return fmt.Errorf("invalid credential ref %q: want env:<group>/<key> or text:<group>/<key>", ref)
	}
	group, key, found := strings.Cut(rest, "/")
	if !found || group == "" || key == "" {
		return fmt.Errorf("invalid credential ref %q: want %s:<group>/<key>", ref, kind)
	}
	if err := storage.ValidateName(group); err != nil {
		return fmt.Errorf("invalid credential ref %q: %w", ref, err)
	}
	if err := storage.ValidateName(key); err != nil {
		return fmt.Errorf("invalid credential ref %q: %w", ref, err)
	}
	return nil
}

// assembleModels 装配模型集：目录模型 ∪ 自定义模型，去重升序，不得为空。
// 每个模型的 context window 优先取显式 ModelContexts，其次取档案已有元数据，
// 最后取 models.dev 目录；RequireModelMetadata 为 true 时缺一项即报错。
// 默认推理档优先级：显式 per-model > 档案已有 > 集合级 DefaultReasoning > 目录。
// 集合级只填充有档位且尚未解析出默认档的模型；有档位缺默认档在
// RequireModelMetadata 或本次改动档位/默认档时拒绝。senv 不从档位列表推断。
func (m *ProviderManager) assembleModels(opts AddProviderOptions) ([]string, map[string]storage.LLMModelInfo, []string, error) {
	var warnings []string
	set := map[string]struct{}{}
	metadata := map[string]ModelMetadata{}
	for id, info := range opts.BaseMetadata {
		metadata[id] = modelMetadataFromStorage(info)
	}

	if opts.CatalogProvider != "" {
		cat, err := Load(opts.CatalogPath)
		if err != nil {
			if errors.Is(err, ErrCacheNotFound) {
				return nil, nil, nil, fmt.Errorf("model catalog cache missing; run `senv ai refresh` first")
			}
			return nil, nil, nil, fmt.Errorf("load model catalog: %w; run `senv ai refresh` to fix", err)
		}
		ids, err := cat.ProviderModelIDs(opts.CatalogProvider)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%w; run `senv ai refresh` if the provider was added upstream", err)
		}
		for _, id := range ids {
			set[id] = struct{}{}
		}
		if at, err := cat.FetchedAtTime(); err == nil && time.Since(at) > catalogStaleAfter {
			warnings = append(warnings, fmt.Sprintf(
				"模型目录缓存已超过 7 天（拉取于 %s），建议执行 senv ai refresh",
				at.Local().Format("2006-01-02")))
		}
	}
	for _, id := range opts.Models {
		if id = strings.TrimSpace(id); id != "" {
			set[id] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil, nil, nil, fmt.Errorf("model set is empty; provide --catalog-provider or --model")
	}

	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// 先读目录元数据，再叠加档案里已有的值和调用方显式值。显式值优先，
	// 保证用户可以用 --model-context 修正目录缺失或过时的数据。
	// 目录默认推理档先抽出，装配末尾再按优先级填入，避免盖过集合级声明。
	catalogDefaultReasoning := map[string]string{}
	if opts.CatalogProvider != "" {
		for id, catalogMeta := range LoadModelMetadata(opts.CatalogPath, opts.CatalogProvider, ids) {
			if catalogMeta.DefaultReasoning != "" {
				catalogDefaultReasoning[id] = catalogMeta.DefaultReasoning
				catalogMeta.DefaultReasoning = ""
			}
			metadata[id] = mergeModelMetadata(catalogMeta, metadata[id])
		}
	}
	// 显式清空：空非 nil map 表示调用方要求移除该维度的全部既有元数据
	// （nil 是「未提供」，非空 map 是增量覆盖）。重置在目录合并之后、
	// override 之前执行，防止档案与目录旧值回填吞掉编辑入口的清空意图。
	if len(opts.ModelOutputs) == 0 && opts.ModelOutputs != nil {
		clearMetadataDimension(metadata, func(meta *ModelMetadata) { meta.OutputLimit = 0 })
	}
	if len(opts.ModelReasoning) == 0 && opts.ModelReasoning != nil {
		clearMetadataDimension(metadata, func(meta *ModelMetadata) { meta.ReasoningEfforts = nil })
	}
	if len(opts.ModelContexts) == 0 && opts.ModelContexts != nil {
		clearMetadataDimension(metadata, func(meta *ModelMetadata) { meta.ContextLimit = 0 })
	}
	if len(opts.ModelDefaultReasoning) == 0 && opts.ModelDefaultReasoning != nil {
		clearMetadataDimension(metadata, func(meta *ModelMetadata) { meta.DefaultReasoning = "" })
	}
	if len(opts.ModelModalities) == 0 && opts.ModelModalities != nil {
		clearMetadataDimension(metadata, func(meta *ModelMetadata) { meta.InputModalities = nil })
	}
	for model, output := range opts.ModelOutputs {
		model = strings.TrimSpace(model)
		if output <= 0 {
			return nil, nil, nil, fmt.Errorf("model %q output limit must be a positive integer", model)
		}
		if _, ok := set[model]; !ok {
			return nil, nil, nil, fmt.Errorf("--model-output model %q is not in the final model set", model)
		}
		metadata[model] = mergeModelMetadata(metadata[model], ModelMetadata{OutputLimit: output})
	}
	for model, efforts := range opts.ModelReasoning {
		model = strings.TrimSpace(model)
		cleaned := make([]string, 0, len(efforts))
		for _, effort := range efforts {
			if effort = strings.TrimSpace(effort); effort != "" {
				cleaned = append(cleaned, effort)
			}
		}
		if len(cleaned) == 0 {
			return nil, nil, nil, fmt.Errorf("model %q reasoning efforts must not be empty", model)
		}
		if _, ok := set[model]; !ok {
			return nil, nil, nil, fmt.Errorf("--model-reasoning model %q is not in the final model set", model)
		}
		metadata[model] = mergeModelMetadata(metadata[model], ModelMetadata{ReasoningEfforts: cleaned})
	}
	for model, contextWindow := range opts.ModelContexts {
		model = strings.TrimSpace(model)
		if contextWindow <= 0 {
			return nil, nil, nil, fmt.Errorf("model %q context window must be a positive integer", model)
		}
		if _, ok := set[model]; !ok {
			return nil, nil, nil, fmt.Errorf("--model-context model %q is not in the final model set", model)
		}
		metadata[model] = mergeModelMetadata(metadata[model], ModelMetadata{ContextLimit: contextWindow})
	}
	for model, effort := range opts.ModelDefaultReasoning {
		model = strings.TrimSpace(model)
		effort = strings.TrimSpace(effort)
		if effort == "" {
			return nil, nil, nil, fmt.Errorf("model %q default reasoning must not be empty", model)
		}
		if _, ok := set[model]; !ok {
			return nil, nil, nil, fmt.Errorf("--model-default-reasoning model %q is not in the final model set", model)
		}
		metadata[model] = mergeModelMetadata(metadata[model], ModelMetadata{DefaultReasoning: effort})
	}
	for model, mods := range opts.ModelModalities {
		model = strings.TrimSpace(model)
		if _, ok := set[model]; !ok {
			return nil, nil, nil, fmt.Errorf("--model-modalities model %q is not in the final model set", model)
		}
		cleaned, err := normalizeInputModalities(mods)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("model %q: %w", model, err)
		}
		metadata[model] = mergeModelMetadata(metadata[model], ModelMetadata{InputModalities: cleaned})
	}

	collectionDefault := strings.TrimSpace(opts.DefaultReasoning)
	for _, id := range ids {
		meta := metadata[id]
		if meta.DefaultReasoning == "" && len(meta.ReasoningEfforts) > 0 {
			if collectionDefault != "" {
				meta.DefaultReasoning = collectionDefault
			} else if catalogDefaultReasoning[id] != "" {
				meta.DefaultReasoning = catalogDefaultReasoning[id]
			}
			metadata[id] = meta
		}
	}

	var missing []string
	if opts.RequireModelMetadata {
		for _, id := range ids {
			if metadata[id].ContextLimit <= 0 {
				missing = append(missing, id)
			}
		}
	}
	if len(missing) > 0 {
		quoted := make([]string, len(missing))
		for i, id := range missing {
			quoted[i] = fmt.Sprintf("%q", id)
		}
		return nil, nil, nil, fmt.Errorf(
			"model(s) %s are missing context window metadata; pass --model-context <model>=<tokens>, or use a catalog model whose models.dev entry includes limit.context",
			strings.Join(quoted, ", "))
	}

	// 显式清空默认推理档（空非 nil per-model map）本身即是声明：本次编辑
	// 造成的「有档位但缺默认档」缺口 MUST NOT 再被必填报错拦下，否则
	// 编辑入口永远无法清空默认推理档。
	defaultReasoningExplicit := opts.ModelDefaultReasoning != nil
	requireDefault := (opts.RequireModelMetadata && !defaultReasoningExplicit) ||
		len(opts.ModelReasoning) > 0 || len(opts.ModelDefaultReasoning) > 0 || collectionDefault != ""
	var missingDefault []string
	for _, id := range ids {
		meta := metadata[id]
		if meta.DefaultReasoning == "" {
			if requireDefault && len(meta.ReasoningEfforts) > 0 {
				missingDefault = append(missingDefault, id)
			}
			continue
		}
		if len(meta.ReasoningEfforts) == 0 || !slices.Contains(meta.ReasoningEfforts, meta.DefaultReasoning) {
			return nil, nil, nil, fmt.Errorf(
				"model %q default reasoning %q is not in reasoning efforts %s",
				id, meta.DefaultReasoning, strings.Join(meta.ReasoningEfforts, ";"))
		}
	}
	if len(missingDefault) > 0 {
		quoted := make([]string, len(missingDefault))
		for i, id := range missingDefault {
			quoted[i] = fmt.Sprintf("%q", id)
		}
		return nil, nil, nil, fmt.Errorf(
			"model(s) %s declare reasoning efforts but have no default reasoning; pass --model-default-reasoning <model>=<effort> or --default-reasoning <effort>",
			strings.Join(quoted, ", "))
	}

	modelInfo := make(map[string]storage.LLMModelInfo, len(ids))
	for _, id := range ids {
		if meta := metadata[id]; !modelMetadataEmpty(meta) {
			modelInfo[id] = storageModelInfo(meta)
		}
	}
	return ids, modelInfo, warnings, nil
}

// ClearingMap 把 nil map 转为空非 nil map（清空哨兵）。调用方 MUST 只在输入
// 已被显式提供（CLI flag Changed / 表单字段变更）时使用：此时 nil 表示
// 「清空该维度」而非「未提供」。
func ClearingMap[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return map[K]V{}
	}
	return m
}

// ParseModelContexts 解析重复的 --model-context <model>=<tokens> 参数。
func ParseModelContexts(specs []string) (map[string]int, error) {
	out := map[string]int{}
	for _, raw := range specs {
		spec := strings.TrimSpace(raw)
		if spec == "" {
			continue
		}
		model, value, ok := strings.Cut(spec, "=")
		model = strings.TrimSpace(model)
		if !ok || model == "" {
			return nil, fmt.Errorf("invalid --model-context %q: want <model>=<tokens>", raw)
		}
		tokens, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || tokens <= 0 {
			return nil, fmt.Errorf("invalid --model-context %q: tokens must be a positive integer", raw)
		}
		if _, exists := out[model]; exists {
			return nil, fmt.Errorf("duplicate --model-context for model %q", model)
		}
		out[model] = tokens
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// ParseModelOutputs 解析重复的 --model-output <model>=<tokens> 参数。
func ParseModelOutputs(specs []string) (map[string]int, error) {
	out := map[string]int{}
	for _, raw := range specs {
		spec := strings.TrimSpace(raw)
		if spec == "" {
			continue
		}
		model, value, ok := strings.Cut(spec, "=")
		model = strings.TrimSpace(model)
		if !ok || model == "" {
			return nil, fmt.Errorf("invalid --model-output %q: want <model>=<tokens>", raw)
		}
		tokens, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || tokens <= 0 {
			return nil, fmt.Errorf("invalid --model-output %q: tokens must be a positive integer", raw)
		}
		if _, exists := out[model]; exists {
			return nil, fmt.Errorf("duplicate --model-output for model %q", model)
		}
		out[model] = tokens
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// ParseModelReasoning 解析重复的 --model-reasoning <model>=<effort>[;<effort>...]
// 参数。档位列表用分号分隔，避免与参数级逗号分隔符冲突。
func ParseModelReasoning(specs []string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, raw := range specs {
		spec := strings.TrimSpace(raw)
		if spec == "" {
			continue
		}
		model, value, ok := strings.Cut(spec, "=")
		model = strings.TrimSpace(model)
		if !ok || model == "" {
			return nil, fmt.Errorf("invalid --model-reasoning %q: want <model>=<effort>[;<effort>...]", raw)
		}
		var efforts []string
		for _, effort := range strings.Split(value, ";") {
			if effort = strings.TrimSpace(effort); effort != "" {
				efforts = append(efforts, effort)
			}
		}
		if len(efforts) == 0 {
			return nil, fmt.Errorf("invalid --model-reasoning %q: want <model>=<effort>[;<effort>...]", raw)
		}
		if _, exists := out[model]; exists {
			return nil, fmt.Errorf("duplicate --model-reasoning for model %q", model)
		}
		out[model] = efforts
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// ParseModelDefaultReasoning 解析重复的 --model-default-reasoning <model>=<effort>。
func ParseModelDefaultReasoning(specs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, raw := range specs {
		spec := strings.TrimSpace(raw)
		if spec == "" {
			continue
		}
		model, value, ok := strings.Cut(spec, "=")
		model = strings.TrimSpace(model)
		effort := strings.TrimSpace(value)
		if !ok || model == "" || effort == "" {
			return nil, fmt.Errorf("invalid --model-default-reasoning %q: want <model>=<effort>", raw)
		}
		if _, exists := out[model]; exists {
			return nil, fmt.Errorf("duplicate --model-default-reasoning for model %q", model)
		}
		out[model] = effort
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

var knownInputModalities = map[string]struct{}{
	"text": {}, "image": {}, "audio": {}, "video": {}, "pdf": {},
}

// ParseModelModalities 解析重复的 --model-modalities <model>=<mod>[,<mod>...]
// 参数。模态以逗号或分号分隔。
func ParseModelModalities(specs []string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, raw := range specs {
		spec := strings.TrimSpace(raw)
		if spec == "" {
			continue
		}
		model, value, ok := strings.Cut(spec, "=")
		model = strings.TrimSpace(model)
		if !ok || model == "" {
			return nil, fmt.Errorf("invalid --model-modalities %q: want <model>=<mod>[,<mod>...]", raw)
		}
		var mods []string
		for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' }) {
			if part = strings.TrimSpace(part); part != "" {
				mods = append(mods, part)
			}
		}
		cleaned, err := normalizeInputModalities(mods)
		if err != nil {
			return nil, fmt.Errorf("invalid --model-modalities %q: %w", raw, err)
		}
		if _, exists := out[model]; exists {
			return nil, fmt.Errorf("duplicate --model-modalities for model %q", model)
		}
		out[model] = cleaned
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func normalizeInputModalities(mods []string) ([]string, error) {
	cleaned := make([]string, 0, len(mods))
	seen := map[string]struct{}{}
	for _, mod := range mods {
		mod = strings.ToLower(strings.TrimSpace(mod))
		if mod == "" {
			continue
		}
		if _, ok := knownInputModalities[mod]; !ok {
			return nil, fmt.Errorf("invalid input modality %q; want text, image, audio, video, or pdf", mod)
		}
		if _, dup := seen[mod]; dup {
			continue
		}
		seen[mod] = struct{}{}
		cleaned = append(cleaned, mod)
	}
	if len(cleaned) == 0 {
		return nil, fmt.Errorf("input modalities must not be empty")
	}
	return cleaned, nil
}

// GetProvider 加载单个档案；不存在时返回带友好文案的错误。
func (m *ProviderManager) GetProvider(alias string) (*storage.LLMProviderEntry, error) {
	entry, err := m.load(alias)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("provider %q not found", alias)
	}
	return entry, err
}

// ListProviders 返回全部档案（按别名升序）。档案不含凭据明文。
func (m *ProviderManager) ListProviders() ([]*storage.LLMProviderEntry, error) {
	aliases, err := m.storage.ListLLMProviders()
	if err != nil {
		return nil, err
	}
	sort.Strings(aliases)
	entries := make([]*storage.LLMProviderEntry, 0, len(aliases))
	for _, alias := range aliases {
		entry, err := m.load(alias)
		if err != nil {
			return nil, fmt.Errorf("load provider %q: %w", alias, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// RenameProviderResult describes a successful provider rename for CLI/TUI.
type RenameProviderResult struct {
	Entry           *storage.LLMProviderEntry
	PointersUpdated int
	EnvRefsUpdated  int
	// PointerAgents lists agent ids whose provider field was rewritten.
	PointerAgents []string
}

// RemoveProviderResult describes credential cleanup for CLI output.
type RemoveProviderResult struct {
	CredentialRemoved bool
	CredentialMissing bool
}

// RenameProvider 将档案主键从 oldAlias 改为 newAlias，并在同一 vault mutation
// 内联动规范自有凭据；随后更新本机 agent 指针中的 provider 字段。pointerPath
// 为空时使用 DefaultPointerPath(home)。MUST NOT 改写 coding agent 原生配置。
func (m *ProviderManager) RenameProvider(oldAlias, newAlias, pointerPath string) (*RenameProviderResult, error) {
	oldAlias = strings.TrimSpace(oldAlias)
	newAlias = strings.TrimSpace(newAlias)
	if err := storage.ValidateName(oldAlias); err != nil {
		return nil, fmt.Errorf("invalid provider alias %q: %w", oldAlias, err)
	}
	if err := storage.ValidateName(newAlias); err != nil {
		return nil, fmt.Errorf("invalid provider alias %q: %w", newAlias, err)
	}
	if oldAlias == newAlias {
		entry, err := m.GetProvider(oldAlias)
		if err != nil {
			return nil, err
		}
		return &RenameProviderResult{Entry: entry}, nil
	}

	var entry *storage.LLMProviderEntry
	var envRefsUpdated int
	err := m.mutate(func(locked *ProviderManager) error {
		current, loadErr := locked.load(oldAlias)
		if errors.Is(loadErr, os.ErrNotExist) {
			return fmt.Errorf("provider %q not found", oldAlias)
		}
		if loadErr != nil {
			return loadErr
		}
		if _, err := locked.load(newAlias); err == nil {
			return fmt.Errorf("provider %q already exists", newAlias)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}

		renamed := *current
		renamed.Alias = newAlias
		renamed.UpdatedAt = time.Now().Truncate(time.Second).UTC()

		tm := locked.textManager()
		em := locked.envManager()
		textRenamed := false
		owned := current.CredentialRef == OwnedCredentialRef(oldAlias)
		var envRollback func()
		if owned {
			if _, err := tm.Get(LLMKeysGroup, newAlias); err == nil {
				return fmt.Errorf("credential text:%s/%s already exists", LLMKeysGroup, newAlias)
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("check target credential: %w", err)
			}
			if _, err := tm.Get(LLMKeysGroup, oldAlias); err == nil {
				if err := tm.RenameKey(LLMKeysGroup, oldAlias, newAlias); err != nil {
					return fmt.Errorf("rename credential: %w", err)
				}
				textRenamed = true
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("read credential: %w", err)
			}
			renamed.CredentialRef = OwnedCredentialRef(newAlias)

			oldRef := ownedSeedRefTemplate(oldAlias)
			applied, cascadeErr := cascadeEnvOwnedSeedRefs(em, oldAlias, newAlias)
			if cascadeErr != nil {
				if textRenamed {
					_ = tm.RenameKey(LLMKeysGroup, newAlias, oldAlias)
				}
				return cascadeErr
			}
			envRefsUpdated = len(applied)
			envRollback = func() { rollbackEnvSeedRefUpdates(em, oldRef, applied) }
		}

		rollbackText := func() {
			if textRenamed {
				_ = tm.RenameKey(LLMKeysGroup, newAlias, oldAlias)
			}
			if envRollback != nil {
				envRollback()
			}
		}
		if err := locked.save(newAlias, &renamed); err != nil {
			rollbackText()
			return fmt.Errorf("save renamed provider: %w", err)
		}
		if err := locked.storage.DeleteLLMProvider(oldAlias); err != nil {
			_ = locked.storage.DeleteLLMProvider(newAlias)
			rollbackText()
			return fmt.Errorf("remove old provider %q: %w", oldAlias, err)
		}
		entry = &renamed
		return nil
	})
	if err != nil {
		return nil, err
	}

	updated, agents, ptrErr := updateProviderPointers(pointerPath, oldAlias, newAlias)
	if ptrErr != nil {
		return &RenameProviderResult{Entry: entry, PointersUpdated: updated, EnvRefsUpdated: envRefsUpdated, PointerAgents: agents},
			fmt.Errorf("provider renamed to %q but agent pointers were not updated: %w", newAlias, ptrErr)
	}
	return &RenameProviderResult{Entry: entry, PointersUpdated: updated, EnvRefsUpdated: envRefsUpdated, PointerAgents: agents}, nil
}

func ownedSeedRefTemplate(alias string) string {
	return fmt.Sprintf("{{text:%s:%s}}", LLMKeysGroup, alias)
}

type envSeedRefUpdate struct {
	group string
	key   string
}

func cascadeEnvOwnedSeedRefs(em *env.Manager, oldAlias, newAlias string) ([]envSeedRefUpdate, error) {
	oldRef := ownedSeedRefTemplate(oldAlias)
	newRef := ownedSeedRefTemplate(newAlias)
	vars, groups, err := em.Snapshot()
	if err != nil {
		return nil, fmt.Errorf("scan env references: %w", err)
	}
	var pending []envSeedRefUpdate
	for _, g := range groups {
		for key, value := range vars[g.Name] {
			if value == oldRef {
				pending = append(pending, envSeedRefUpdate{group: g.Name, key: key})
			}
		}
	}
	applied := make([]envSeedRefUpdate, 0, len(pending))
	for _, u := range pending {
		if err := em.Set(u.group, u.key, newRef); err != nil {
			rollbackEnvSeedRefUpdates(em, oldRef, applied)
			return nil, fmt.Errorf("update env reference %s in group %s: %w", u.key, u.group, err)
		}
		applied = append(applied, u)
	}
	return applied, nil
}

func rollbackEnvSeedRefUpdates(em *env.Manager, oldRef string, applied []envSeedRefUpdate) {
	for _, u := range applied {
		_ = em.Set(u.group, u.key, oldRef)
	}
}

// updateProviderPointers rewrites every agent pointer whose provider matches
// oldAlias. A missing pointer file is a no-op (0 updates).
func updateProviderPointers(pointerPath, oldAlias, newAlias string) (int, []string, error) {
	if pointerPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, nil, fmt.Errorf("resolve home directory: %w", err)
		}
		pointerPath = DefaultPointerPath(home)
	}
	pf, err := LoadPointers(pointerPath)
	if errors.Is(err, ErrPointerNotFound) {
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, err
	}
	var agents []string
	for id, p := range pf.Agents {
		if p.Provider != oldAlias {
			continue
		}
		p.Provider = newAlias
		pf.Agents[id] = p
		agents = append(agents, id)
	}
	if len(agents) == 0 {
		return 0, nil, nil
	}
	sort.Strings(agents)
	if err := SavePointers(pointerPath, pf); err != nil {
		return 0, nil, err
	}
	return len(agents), agents, nil
}

// RemoveProvider 先处理自有凭据，再删除档案；档案删除失败时用旧值补偿凭据。
func (m *ProviderManager) RemoveProvider(alias string) (RemoveProviderResult, error) {
	var result RemoveProviderResult
	err := m.mutate(func(locked *ProviderManager) error {
		entry, loadErr := locked.load(alias)
		if errors.Is(loadErr, os.ErrNotExist) {
			return fmt.Errorf("provider %q not found", alias)
		}
		if loadErr != nil {
			return loadErr
		}
		if entry.CredentialRef != OwnedCredentialRef(alias) {
			if locked.removeProfile == nil {
				return locked.storage.DeleteLLMProvider(alias)
			}
			return locked.removeProfile()
		}

		oldValue, getErr := locked.textManager().Get(LLMKeysGroup, alias)
		credExists := getErr == nil
		if getErr != nil && !errors.Is(getErr, os.ErrNotExist) {
			return fmt.Errorf("read credential: %w", getErr)
		}
		var delErr error
		if locked.removeCredential == nil {
			delErr = locked.textManager().Delete(LLMKeysGroup, alias)
		} else {
			delErr = locked.removeCredential()
		}
		if errors.Is(delErr, os.ErrNotExist) {
			result.CredentialMissing = true
		} else if delErr != nil {
			return fmt.Errorf("delete credential; provider kept for retry: %w", delErr)
		} else if credExists {
			result.CredentialRemoved = true
		} else {
			result.CredentialMissing = true
		}

		var deleteErr error
		if locked.removeProfile == nil {
			deleteErr = locked.storage.DeleteLLMProvider(alias)
		} else {
			deleteErr = locked.removeProfile()
		}
		if deleteErr != nil {
			if credExists {
				if restoreErr := locked.textManager().Set(LLMKeysGroup, alias, oldValue); restoreErr != nil {
					return fmt.Errorf("delete provider: %v; restore credential also failed: %w", deleteErr, restoreErr)
				}
			}
			result = RemoveProviderResult{}
			return deleteErr
		}
		return nil
	})
	return result, err
}
