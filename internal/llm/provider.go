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
		return fn(&clone)
	})
}

func (m *ProviderManager) textManager() *text.Manager {
	if m.key != nil {
		return text.NewManagerWithKey(m.storage, m.key)
	}
	return text.NewManager(m.storage, m.password)
}

// envManager 返回 env 凭据读取器（env: 引用解密用）。
func (m *ProviderManager) envManager() *env.Manager {
	if m.key != nil {
		return env.NewManagerWithKey(m.storage, m.key)
	}
	return env.NewManager(m.storage, m.password)
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
	DefaultModel    string
	Force           bool
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
	baseURL := strings.TrimSpace(opts.BaseURL)
	if err := storage.ValidateLLMProviderURL(baseURL, opts.AllowHTTP); err != nil {
		return nil, fmt.Errorf("invalid base URL %q: %w", opts.BaseURL, err)
	}
	if err := validateCredentialInput(opts); err != nil {
		return nil, err
	}
	models, warnings, err := m.assembleModels(opts)
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
		Models:          models,
		DefaultModel:    defaultModel,
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
	return &AddProviderResult{Entry: entry, Warnings: warnings}, nil
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
// 返回值中的 warnings 是非致命提示（如缓存过期）。
func (m *ProviderManager) assembleModels(opts AddProviderOptions) ([]string, []string, error) {
	var warnings []string
	set := map[string]struct{}{}
	if opts.CatalogProvider != "" {
		cat, err := Load(opts.CatalogPath)
		if err != nil {
			if errors.Is(err, ErrCacheNotFound) {
				return nil, nil, fmt.Errorf("model catalog cache missing; run `senv ai refresh` first")
			}
			return nil, nil, fmt.Errorf("load model catalog: %w; run `senv ai refresh` to fix", err)
		}
		ids, err := cat.ProviderModelIDs(opts.CatalogProvider)
		if err != nil {
			return nil, nil, fmt.Errorf("%w; run `senv ai refresh` if the provider was added upstream", err)
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
		return nil, nil, fmt.Errorf("model set is empty; provide --catalog-provider or --model")
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, warnings, nil
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

// RemoveProviderResult describes credential cleanup for CLI output.
type RemoveProviderResult struct {
	CredentialRemoved bool
	CredentialMissing bool
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
