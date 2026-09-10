// Package llm 提供 LLM 接入相关能力。本文件实现 models.dev 模型目录的
// 拉取、校验与本地缓存：目录是机器本地的公开数据，不进 vault、不含敏感信息。
package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// catalogVersion 是缓存 envelope 的格式版本；布局变化时递增。
const catalogVersion = 1

// DefaultCatalogURL 是 models.dev 公开目录地址。
const DefaultCatalogURL = "https://models.dev/api.json"

// maxCatalogBytes 限制响应体大小，防止异常源拖垮内存。
const maxCatalogBytes = 32 << 20 // 32MiB

// 缓存文件与目录权限：目录数据公开，但沿用仓库惯例收紧权限。
const (
	cacheDirMode  = 0o700
	cacheFileMode = 0o600
)

// 缓存相关哨兵错误：调用方用 errors.Is 区分「无缓存」与「缓存损坏」。
var (
	ErrCacheNotFound = errors.New("model catalog cache not found")
	ErrCacheCorrupt  = errors.New("model catalog cache corrupt")
)

// defaultCatalogClient 是刷新用的默认 HTTP client。
var defaultCatalogClient = &http.Client{Timeout: 30 * time.Second}

// Catalog 是落盘缓存的 envelope。providers 透传上游原始 JSON：
// 字段裁剪会在后续功能需要新字段时被迫改缓存格式，得不偿失。
type Catalog struct {
	Version   int             `json:"version"`
	FetchedAt string          `json:"fetched_at"` // RFC3339
	Source    string          `json:"source"`
	Providers json.RawMessage `json:"providers"`
}

// Fetch 从 source 拉取目录并解析校验；client 为 nil 时使用默认 client。
func Fetch(source string, client *http.Client) (*Catalog, error) {
	if client == nil {
		client = defaultCatalogClient
	}
	resp, err := client.Get(source)
	if err != nil {
		return nil, fmt.Errorf("fetch catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch catalog: unexpected status %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fetch catalog: %w", err)
	}
	if len(data) > maxCatalogBytes {
		return nil, fmt.Errorf("fetch catalog: response exceeds %d bytes", maxCatalogBytes)
	}
	return Parse(data, source, time.Now())
}

// Parse 校验目录内容并组装 envelope；data 即 providers 原文。
func Parse(data []byte, source string, fetchedAt time.Time) (*Catalog, error) {
	if _, _, err := validateProviders(data); err != nil {
		return nil, err
	}
	return &Catalog{
		Version:   catalogVersion,
		FetchedAt: fetchedAt.UTC().Format(time.RFC3339),
		Source:    source,
		Providers: json.RawMessage(data),
	}, nil
}

// Counts 返回缓存中的 provider 数与 model 数。
func (c *Catalog) Counts() (providers, models int, err error) {
	return validateProviders(c.Providers)
}

// FetchedAtTime 解析拉取时间。
func (c *Catalog) FetchedAtTime() (time.Time, error) {
	t, err := time.Parse(time.RFC3339, c.FetchedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse fetched_at: %w", err)
	}
	return t, nil
}

// ProviderModelIDs 返回目录中指定 provider 的全部 model id（升序）。
// 供 provider 档案装配模型集使用。
func (c *Catalog) ProviderModelIDs(catalogID string) ([]string, error) {
	var providers map[string]struct {
		Models map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(c.Providers, &providers); err != nil {
		return nil, fmt.Errorf("parse catalog providers: %w", err)
	}
	provider, ok := providers[catalogID]
	if !ok {
		return nil, fmt.Errorf("catalog has no provider %q", catalogID)
	}
	ids := make([]string, 0, len(provider.Models))
	for id := range provider.Models {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("catalog provider %q has no models", catalogID)
	}
	sort.Strings(ids)
	return ids, nil
}

// validateProviders 校验 payload 是非空 provider 对象集合且至少含一个 model，
// 返回 provider 数与 model 数。校验失败即视为目录非法。
func validateProviders(data []byte) (int, int, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return 0, 0, fmt.Errorf("catalog payload is not an object: %w", err)
	}
	if len(raw) == 0 {
		return 0, 0, errors.New("catalog payload contains no providers")
	}
	models := 0
	for id, value := range raw {
		if id == "" {
			return 0, 0, errors.New("catalog payload contains empty provider id")
		}
		var provider struct {
			Models map[string]json.RawMessage `json:"models"`
		}
		if err := json.Unmarshal(value, &provider); err != nil {
			return 0, 0, fmt.Errorf("catalog provider %q is not an object: %w", id, err)
		}
		models += len(provider.Models)
	}
	if models == 0 {
		return 0, 0, errors.New("catalog payload contains no models")
	}
	return len(raw), models, nil
}

// Save 将目录以 temp+rename 原子写入 path；失败时不触碰旧文件。
func Save(path string, c *Catalog) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode catalog: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, cacheDirMode); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".models-dev-*.tmp")
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
	if err := tmp.Chmod(cacheFileMode); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replace catalog cache: %w", err)
	}
	return nil
}

// Load 读取并校验缓存。不存在返回 ErrCacheNotFound；内容无法识别
// （JSON 损坏、版本不符、结构非法）返回 ErrCacheCorrupt。
func Load(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrCacheNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read catalog cache: %w", err)
	}
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCacheCorrupt, err)
	}
	if c.Version != catalogVersion {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrCacheCorrupt, c.Version)
	}
	if _, err := c.FetchedAtTime(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCacheCorrupt, err)
	}
	if _, _, err := validateProviders(c.Providers); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCacheCorrupt, err)
	}
	return &c, nil
}
