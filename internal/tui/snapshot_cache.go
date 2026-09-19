package tui

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/securefs"
	"github.com/wii/senv/internal/storage"
)

// EnvSnapshotOff 是快照缓存的功能开关：SENV_TUI_SNAPSHOT=off 时整体回退到
// 直接解密路径（启动不预热、退出/写后不落盘）。
const EnvSnapshotOff = "SENV_TUI_SNAPSHOT"

const (
	// snapshotCacheFileName 是快照缓存文件名（<dataPath>/tui-snapshot.enc）。
	snapshotCacheFileName = "tui-snapshot.enc"
	// snapshotCacheVersion 是缓存载荷格式版本：不兼容演进时递增，旧文件按
	// 未命中处理（静默回退直接解密路径）。
	snapshotCacheVersion = 1
)

// SnapshotCache 是 TUI 明文列表（env 变量 + text 元数据展示视图）的加密
// 磁盘缓存：载荷序列化后用 vault 主密钥 AES-256-GCM 加密落盘，权限 0600。
// 静态安全级别与 vault 条目等同（同密钥、同算法、同权限位），攻击者能读
// 快照密文即已能读 vault 密文，不扩大攻击面。任何读/写失败都静默回退
// 直接解密路径（tui-startup-perf D4）。
type SnapshotCache struct {
	dataPath   string
	configPath string
	key        []byte
}

// NewSnapshotCache 构造快照缓存；key 为 vault 主密钥（PBKDF2 派生密钥）。
// 以下情况返回 nil（功能整体关闭，消费方按 nil 处理）：key 为空、
// SENV_TUI_SNAPSHOT=off。
func NewSnapshotCache(dataPath, configPath string, key []byte) *SnapshotCache {
	if key == nil || os.Getenv(EnvSnapshotOff) == "off" {
		return nil
	}
	return &SnapshotCache{dataPath: dataPath, configPath: configPath, key: key}
}

// snapshotCachePayload 是缓存文件的明文结构：全域展示视图 + staleness
// 指纹。env 含变量明文（与运行态内存一致）；text 只含条目元数据（tab
// 只渲染元数据）。指纹不含明文/密钥材料。
type snapshotCachePayload struct {
	Version     int         `json:"version"`
	Fingerprint string      `json:"fingerprint"`
	Env         *envSnap    `json:"env"`
	Text        *textSnap   `json:"text"`
	Backup      *backupSnap `json:"backup,omitempty"`
}

// gatherSnapshot 采集当前展示视图：优先复用进程内 memo（明文本就在内存，
// 零额外解密），memo 不可用时经 Manager 现读。任一域不可用即放弃本次写入。
func gatherSnapshot(mgr Managers) (*snapshotCachePayload, error) {
	var envData *envSnap
	var textData *textSnap
	var backupData *backupSnap
	if mgr.snap != nil {
		// Get/GetText 在 memo 被失效后会现场重建，拿到的总是当前真相。
		if s, err := mgr.snap.Get(); err == nil {
			envData = s
		}
		if s, err := mgr.snap.GetText(); err == nil {
			textData = s
		}
		if mgr.Backup != nil {
			if s, err := mgr.snap.GetBackup(); err == nil {
				backupData = s
			}
		}
	}
	if envData == nil {
		if mgr.Env == nil {
			return nil, fmt.Errorf("env manager unavailable")
		}
		vars, groups, err := mgr.Env.Snapshot()
		if err != nil {
			return nil, err
		}
		envData = &envSnap{Vars: vars, Groups: groups}
	}
	if textData == nil {
		if mgr.Text == nil {
			return nil, fmt.Errorf("text manager unavailable")
		}
		snap, err := mgr.Text.Snapshot()
		if err != nil {
			return nil, err
		}
		textData = &textSnap{Groups: snap.Groups, Items: snap.Items}
	}
	if backupData == nil && mgr.Backup != nil {
		snap, err := mgr.Backup.Snapshot()
		if err != nil {
			return nil, err
		}
		backupData = &backupSnap{Groups: snap.Groups, Items: snap.Items}
	}
	return &snapshotCachePayload{
		Version: snapshotCacheVersion,
		Env:     envData,
		Text:    textData,
		Backup:  backupData,
	}, nil
}

// TryLoad 读取并校验快照缓存：命中返回解密后的载荷，供 registry 预热。
// 文件缺失/损坏/版本不符/指纹不符一律静默返回 nil——调用方回退直接解密
// 路径，不报错、不渲染错误内容。
func (c *SnapshotCache) TryLoad() *snapshotCachePayload {
	st := perflog.Start("tui.snapshot-read")
	payload, ok := c.load()
	st.With("hit", ok).End(ok)
	if !ok {
		return nil
	}
	return payload
}

func (c *SnapshotCache) load() (*snapshotCachePayload, bool) {
	root, err := securefs.OpenRoot(c.dataPath)
	if err != nil {
		return nil, false
	}
	defer root.Close()
	blob, err := root.Read(snapshotCacheFileName)
	if err != nil {
		return nil, false
	}
	plain, err := crypto.Decrypt(c.key, string(blob))
	if err != nil {
		return nil, false
	}
	var payload snapshotCachePayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return nil, false
	}
	if payload.Version != snapshotCacheVersion || payload.Env == nil || payload.Text == nil {
		return nil, false
	}
	fp, err := c.vaultFingerprint()
	if err != nil || fp != payload.Fingerprint {
		return nil, false
	}
	return &payload, true
}

// Write 采集当前展示视图并加密落盘（best-effort：任何失败静默返回 false，
// 绝不影响业务路径）。AtomicWrite 原子替换，权限 0600；目录权限沿用
// dataPath 既有 0700。
func (c *SnapshotCache) Write(mgr Managers) bool {
	st := perflog.Start("tui.snapshot-write")
	ok := c.write(mgr)
	st.End(ok)
	return ok
}

func (c *SnapshotCache) write(mgr Managers) bool {
	payload, err := gatherSnapshot(mgr)
	if err != nil {
		return false
	}
	fp, err := c.vaultFingerprint()
	if err != nil {
		return false
	}
	payload.Fingerprint = fp
	plain, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	blob, err := crypto.Encrypt(c.key, plain)
	if err != nil {
		return false
	}
	root, err := securefs.OpenRoot(c.dataPath)
	if err != nil {
		return false
	}
	defer root.Close()
	return root.AtomicWrite([]string{snapshotCacheFileName}, []byte(blob), 0o600) == nil
}

// vaultFingerprint 计算 vault 密文清单指纹：salt + 全部条目密文文件的
// （路径+size+mtime）聚合（sha256）。只含元数据，不含明文/密钥材料；用于
// 粗判快照 staleness（rekey/换 vault 时 salt 变化自然失配；mtime 被触碰
// 的最坏情况只是多一次回退）。
func (c *SnapshotCache) vaultFingerprint() (string, error) {
	sm := storage.NewManager(c.configPath, c.dataPath)
	md, err := sm.LoadMetadata()
	if err != nil {
		return "", err
	}
	salt, err := base64.StdEncoding.DecodeString(md.Salt)
	if err != nil {
		return "", err
	}
	entries, err := c.manifestEntries()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(salt)
	for _, e := range entries {
		h.Write([]byte(e))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// manifestEntries 枚举参与指纹的密文文件条目（"相对路径\x00size\x00mtime"），
// 排序后返回。dataPath 全量递归（排除机器本地工件，见 storage 登记表）；configPath
// 只含 config_index.json——其余 configPath 文件不喂养快照数据，metadata
// 变化由 salt 自然覆盖，session/agent 指针等频繁变动文件避免指纹抖动。
func (c *SnapshotCache) manifestEntries() ([]string, error) {
	var out []string
	dataRoot, err := securefs.OpenRoot(c.dataPath)
	if err != nil {
		return nil, err
	}
	defer dataRoot.Close()
	if err := walkManifest(dataRoot, nil, &out); err != nil {
		return nil, err
	}

	cfgRoot, err := securefs.OpenRoot(c.configPath)
	if err != nil {
		return nil, err
	}
	defer cfgRoot.Close()
	entries, err := cfgRoot.ReadDir()
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir && e.Name == storage.ConfigIndexFile {
			out = append(out, manifestEntry(storage.ConfigIndexFile, e))
		}
	}
	sort.Strings(out)
	return out, nil
}

// walkManifest 递归枚举 trusted root 下的普通文件（不跟随符号链接），
// dataPath 顶层的机器本地工件（快照、同步 state、锁）排除在指纹外。
func walkManifest(root *securefs.Root, prefix []string, out *[]string) error {
	entries, err := root.ReadDir(prefix...)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir {
			if err := walkManifest(root, append(append([]string{}, prefix...), e.Name), out); err != nil {
				return err
			}
			continue
		}
		if len(prefix) == 0 && storage.IsMachineLocalDataArtifact(e.Name) {
			continue
		}
		*out = append(*out, manifestEntry(strings.Join(append(append([]string{}, prefix...), e.Name), "/"), e))
	}
	return nil
}

func manifestEntry(path string, e securefs.DirEntry) string {
	return fmt.Sprintf("%s\x00%d\x00%d\x00%d", path, e.Size, e.ModSec, e.ModNsec)
}
