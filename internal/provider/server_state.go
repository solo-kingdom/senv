package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 同步条目的 kind 取值（与 server schema 一致）
const (
	KindEnv         = "env"
	KindEnvMeta     = "env_meta"
	KindText        = "text"
	KindConfig      = "config"
	KindConfigIndex = "config_index"
)

// syncStateFileName 同步状态文件名，存于缓存目录（dataPath）内，不进加密区、不含敏感内容
const syncStateFileName = ".senv-sync-state.json"

// syncEntryState 记录条目在上次同步后的本地内容哈希与 server revision
type syncEntryState struct {
	Revision int64  `json:"revision"`
	Hash     string `json:"hash"`
}

// syncState 本地同步状态：last_synced_revision + 快照（dirty 判定依据）。
// LastPullAt 为读路径自动拉取的节流时间戳（unix 秒，旧文件缺省为 0 = 立即拉取）。
type syncState struct {
	LastSyncedRevision int64                     `json:"last_synced_revision"`
	LastPullAt         int64                     `json:"last_pull_at,omitempty"`
	MetadataHash       string                    `json:"metadata_hash"`
	Entries            map[string]syncEntryState `json:"entries"`
}

func newSyncState() *syncState {
	return &syncState{Entries: make(map[string]syncEntryState)}
}

// hashBytes 计算内容的 SHA-256 十六进制（dirty 判定用，内容本身就是密文）
func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// localCache 封装 server 模式本地缓存目录的条目 <-> 文件映射。
// 布局与现有 storage.Manager 文件格式完全一致（缓存即工作副本）。
type localCache struct {
	configPath string
	dataPath   string
}

func (c *localCache) stateFilePath() string {
	return filepath.Join(c.dataPath, syncStateFileName)
}

// entryPath 返回条目对应的本地文件路径
func (c *localCache) entryPath(kind, grp, key string) string {
	switch kind {
	case KindEnv:
		return filepath.Join(c.dataPath, "envs", grp, key+".enc")
	case KindEnvMeta:
		return filepath.Join(c.dataPath, "envs", grp, ".meta.enc")
	case KindText:
		return filepath.Join(c.dataPath, "texts", grp, key+".enc")
	case KindConfig:
		return filepath.Join(c.dataPath, key+".enc")
	case KindConfigIndex:
		return filepath.Join(c.configPath, "config_index.json")
	}
	return ""
}

// collect 扫描本地缓存，返回全部条目（id -> Entry，含密文内容与哈希）
func (c *localCache) collect() (map[string]Entry, error) {
	entries := make(map[string]Entry)
	add := func(kind, grp, key, path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries[entryID(kind, grp, key)] = Entry{Kind: kind, Grp: grp, Key: key, Ciphertext: data}
		return nil
	}

	// envs/<grp>/*.enc 与 envs/<grp>/.meta.enc
	envsDir := filepath.Join(c.dataPath, "envs")
	if groups, err := os.ReadDir(envsDir); err == nil {
		for _, g := range groups {
			if !g.IsDir() {
				continue
			}
			files, err := os.ReadDir(filepath.Join(envsDir, g.Name()))
			if err != nil {
				return nil, err
			}
			for _, f := range files {
				name := f.Name()
				switch {
				case name == ".meta.enc":
					if err := add(KindEnvMeta, g.Name(), "", filepath.Join(envsDir, g.Name(), name)); err != nil {
						return nil, err
					}
				case strings.HasSuffix(name, ".enc"):
					key := strings.TrimSuffix(name, ".enc")
					if err := add(KindEnv, g.Name(), key, filepath.Join(envsDir, g.Name(), name)); err != nil {
						return nil, err
					}
				}
			}
		}
	}

	// texts/<grp>/<key>.enc
	textsDir := filepath.Join(c.dataPath, "texts")
	if groups, err := os.ReadDir(textsDir); err == nil {
		for _, g := range groups {
			if !g.IsDir() {
				continue
			}
			files, err := os.ReadDir(filepath.Join(textsDir, g.Name()))
			if err != nil {
				return nil, err
			}
			for _, f := range files {
				if strings.HasSuffix(f.Name(), ".enc") {
					key := strings.TrimSuffix(f.Name(), ".enc")
					if err := add(KindText, g.Name(), key, filepath.Join(textsDir, g.Name(), f.Name())); err != nil {
						return nil, err
					}
				}
			}
		}
	}

	// dataPath 根下的 config 密文文件（排除同步状态文件与 env_*.json.enc 旧格式遗留）
	if files, err := os.ReadDir(c.dataPath); err == nil {
		for _, f := range files {
			name := f.Name()
			if f.IsDir() || name == syncStateFileName {
				continue
			}
			if strings.HasSuffix(name, ".json.enc") && strings.HasPrefix(name, "env_") {
				continue // 旧格式遗留文件不参与同步
			}
			if strings.HasSuffix(name, ".enc") {
				key := strings.TrimSuffix(name, ".enc")
				if err := add(KindConfig, "", key, filepath.Join(c.dataPath, name)); err != nil {
					return nil, err
				}
			}
		}
	}

	// config_index.json（位于 configPath）
	indexPath := c.entryPath(KindConfigIndex, "", "")
	if _, err := os.Stat(indexPath); err == nil {
		if err := add(KindConfigIndex, "", "", indexPath); err != nil {
			return nil, err
		}
	}

	return entries, nil
}

// apply 把拉取到的条目落盘（删除则移除文件）；权限与 storage.Manager 一致（目录 700 文件 600）
func (c *localCache) apply(e Entry) error {
	path := c.entryPath(e.Kind, e.Grp, e.Key)
	if path == "" {
		return nil
	}
	if e.Deleted {
		err := os.Remove(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, e.Ciphertext, 0o600)
}

// metadataPath / metadata 读写（vault metadata blob ↔ configPath/metadata.json）
func (c *localCache) metadataPath() string {
	return filepath.Join(c.configPath, "metadata.json")
}

func (c *localCache) readMetadata() ([]byte, error) {
	data, err := os.ReadFile(c.metadataPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

func (c *localCache) writeMetadata(blob []byte) error {
	if err := os.MkdirAll(c.configPath, 0o700); err != nil {
		return err
	}
	return os.WriteFile(c.metadataPath(), blob, 0o600)
}

// loadState / saveState 读写同步状态文件。
// 状态损坏时不重建（空快照会把全部条目当 dirty 以 base_revision=0 推送，
// 触发整批 409），而是返回错误：自动同步静默跳过，手动 sync 明确报错指引删除重建。
func (c *localCache) loadState() (*syncState, error) {
	data, err := os.ReadFile(c.stateFilePath())
	if os.IsNotExist(err) {
		return newSyncState(), nil
	}
	if err != nil {
		return nil, err
	}
	var st syncState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("同步状态文件损坏（%s）: %w；删除该文件后执行 senv init 或 senv sync 可重建", c.stateFilePath(), err)
	}
	if st.Entries == nil {
		st.Entries = make(map[string]syncEntryState)
	}
	return &st, nil
}

func (c *localCache) saveState(st *syncState) error {
	if err := os.MkdirAll(c.dataPath, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.stateFilePath(), data, 0o600)
}
