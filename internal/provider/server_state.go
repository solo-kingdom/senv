package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wii/senv/internal/securefs"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/syncschema"
)

// Keep the provider constants as aliases for compatibility while making the
// shared schema package the single source of truth.
const (
	KindEnv         = syncschema.KindEnv
	KindEnvMeta     = syncschema.KindEnvMeta
	KindText        = syncschema.KindText
	KindConfig      = syncschema.KindConfig
	KindConfigIndex = syncschema.KindConfigIndex
)

const syncStateFileName = ".senv-sync-state.json"

type syncEntryState struct {
	Revision int64  `json:"revision"`
	Hash     string `json:"hash"`
}

type syncState struct {
	LastSyncedRevision int64                     `json:"last_synced_revision"`
	LastPullAt         int64                     `json:"last_pull_at,omitempty"`
	MetadataHash       string                    `json:"metadata_hash"`
	VaultBinding       *vaultBinding             `json:"vault_binding,omitempty"`
	WrittenBy          *stateWriterInfo          `json:"written_by,omitempty"`
	Entries            map[string]syncEntryState `json:"entries"`
}

func newSyncState() *syncState {
	return &syncState{Entries: make(map[string]syncEntryState)}
}

// vaultBinding 是状态文件的 vault 归属标记；Server 为地址指纹（sha256 前 16 hex），不含敏感内容。
type vaultBinding struct {
	Server string `json:"server"`
	Vault  string `json:"vault"`
}

// stateWriterInfo 记录最近一次状态写入来源，用于事后取证（不含敏感内容）。
type stateWriterInfo struct {
	Path string `json:"path"`
	PID  int    `json:"pid"`
	TS   int64  `json:"ts"`
}

// serverFingerprint 计算 server 地址的非可逆指纹，取 sha256 前 16 个 hex 字符。
func serverFingerprint(address string) string {
	sum := sha256.Sum256([]byte(strings.TrimRight(address, "/")))
	return hex.EncodeToString(sum[:])[:16]
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type providerRootOpener func(string) (securefs.TrustedRoot, error)

type localCache struct {
	configPath string
	dataPath   string
	// binding 非空时，loadState 校验状态文件归属并在写入时补全绑定。
	binding *vaultBinding
	// openRoot is a package-private fault seam. Production caches leave it nil.
	openRoot providerRootOpener
}

func (c *localCache) rootOpener() providerRootOpener {
	if c.openRoot != nil {
		return c.openRoot
	}
	return func(path string) (securefs.TrustedRoot, error) { return securefs.OpenRoot(path) }
}

func (c *localCache) openExistingRoot(path string) (securefs.TrustedRoot, error) {
	return c.rootOpener()(path)
}

func (c *localCache) openTrustedRoot(path string) (securefs.TrustedRoot, error) {
	if err := storage.EnsurePrivateDir(path, 0o700); err != nil {
		return nil, err
	}
	return c.rootOpener()(path)
}

func (c *localCache) stateFilePath() string {
	return filepath.Join(c.dataPath, syncStateFileName)
}

func (c *localCache) metadataPath() string {
	return filepath.Join(c.configPath, storage.MetadataFile)
}

type cacheRootKind uint8

const (
	cacheDataRoot cacheRootKind = iota
	cacheConfigRoot
)

type cacheLocation struct {
	root     cacheRootKind
	segments []string
}

func (c *localCache) entryLocation(kind, grp, key string) (cacheLocation, error) {
	if err := syncschema.ValidateIdentity(kind, grp, key); err != nil {
		return cacheLocation{}, fmt.Errorf("remote entry identity rejected: %w", err)
	}
	switch kind {
	case KindEnv:
		return cacheLocation{root: cacheDataRoot, segments: []string{storage.EnvDirName, grp, key + storage.EnvVarSuffix}}, nil
	case KindEnvMeta:
		return cacheLocation{root: cacheDataRoot, segments: []string{storage.EnvDirName, grp, storage.EnvMetaFileName}}, nil
	case KindText:
		return cacheLocation{root: cacheDataRoot, segments: []string{storage.TextDirName, grp, key + storage.TextFileSuffix}}, nil
	case KindConfig:
		return cacheLocation{root: cacheDataRoot, segments: []string{key + storage.ConfigFileSuffix}}, nil
	case KindConfigIndex:
		return cacheLocation{root: cacheConfigRoot, segments: []string{storage.ConfigIndexFile}}, nil
	default:
		panic("syncschema accepted unknown kind")
	}
}

// entryPath maps a validated entry identity to its compatibility filesystem
// path. All mutations use entryLocation with trusted roots instead.
func (c *localCache) entryPath(kind, grp, key string) (string, error) {
	location, err := c.entryLocation(kind, grp, key)
	if err != nil {
		return "", err
	}
	base := c.dataPath
	if location.root == cacheConfigRoot {
		base = c.configPath
	}
	return filepath.Join(append([]string{base}, location.segments...)...), nil
}

func validateRemoteEntries(entries []Entry) error {
	for _, entry := range entries {
		if err := syncschema.ValidateIdentity(entry.Kind, entry.Grp, entry.Key); err != nil {
			return fmt.Errorf("remote entry identity rejected: %w", err)
		}
	}
	return nil
}

// collect enumerates the cache through trusted roots. Invalid historical names,
// symlinks, and special files fail closed rather than being followed or skipped.
func (c *localCache) collect() (map[string]Entry, error) {
	entries := make(map[string]Entry)
	dataRoot, err := c.openExistingRoot(c.dataPath)
	if err != nil {
		return nil, err
	}
	defer dataRoot.Close()

	add := func(kind, grp, key string, root securefs.TrustedRoot, segments ...string) error {
		if err := syncschema.ValidateIdentity(kind, grp, key); err != nil {
			return fmt.Errorf("local sync entry identity rejected: %w", err)
		}
		data, err := root.Read(segments...)
		if err != nil {
			return err
		}
		entries[entryID(kind, grp, key)] = Entry{Kind: kind, Grp: grp, Key: key, Ciphertext: data}
		return nil
	}

	if groups, err := dataRoot.ReadDir(storage.EnvDirName); err == nil {
		for _, group := range groups {
			if !group.IsDir {
				return nil, fmt.Errorf("invalid env cache entry type")
			}
			files, err := dataRoot.ReadDir(storage.EnvDirName, group.Name)
			if err != nil {
				return nil, err
			}
			for _, file := range files {
				if file.IsDir {
					return nil, fmt.Errorf("invalid env cache entry type")
				}
				switch {
				case file.Name == storage.EnvMetaFileName:
					if err := add(KindEnvMeta, group.Name, "", dataRoot, storage.EnvDirName, group.Name, file.Name); err != nil {
						return nil, err
					}
				case strings.HasSuffix(file.Name, storage.EnvVarSuffix):
					key := strings.TrimSuffix(file.Name, storage.EnvVarSuffix)
					if err := add(KindEnv, group.Name, key, dataRoot, storage.EnvDirName, group.Name, file.Name); err != nil {
						return nil, err
					}
				}
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if groups, err := dataRoot.ReadDir(storage.TextDirName); err == nil {
		for _, group := range groups {
			if !group.IsDir {
				return nil, fmt.Errorf("invalid text cache entry type")
			}
			files, err := dataRoot.ReadDir(storage.TextDirName, group.Name)
			if err != nil {
				return nil, err
			}
			for _, file := range files {
				if file.IsDir {
					return nil, fmt.Errorf("invalid text cache entry type")
				}
				if strings.HasSuffix(file.Name, storage.TextFileSuffix) {
					key := strings.TrimSuffix(file.Name, storage.TextFileSuffix)
					if err := add(KindText, group.Name, key, dataRoot, storage.TextDirName, group.Name, file.Name); err != nil {
						return nil, err
					}
				}
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	files, err := dataRoot.ReadDir()
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		name := file.Name
		if file.IsDir || name == syncStateFileName ||
			(strings.HasSuffix(name, storage.EnvFileSuffix) && strings.HasPrefix(name, storage.EnvFilePrefix)) {
			continue
		}
		if strings.HasSuffix(name, storage.ConfigFileSuffix) {
			key := strings.TrimSuffix(name, storage.ConfigFileSuffix)
			if err := add(KindConfig, "", key, dataRoot, name); err != nil {
				return nil, err
			}
		}
	}

	configRoot, err := c.openExistingRoot(c.configPath)
	if err != nil {
		return nil, err
	}
	defer configRoot.Close()
	if _, err := configRoot.Read(storage.ConfigIndexFile); err == nil {
		if err := add(KindConfigIndex, "", "", configRoot, storage.ConfigIndexFile); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return entries, nil
}

type cacheSnapshot struct {
	location cacheLocation
	data     []byte
	exists   bool
}

type cacheTransaction struct {
	cache      *localCache
	dataRoot   securefs.TrustedRoot
	configRoot securefs.TrustedRoot
	snapshots  map[string]cacheSnapshot
	order      []string
}

func newCacheTransaction(cache *localCache) *cacheTransaction {
	return &cacheTransaction{cache: cache, snapshots: make(map[string]cacheSnapshot)}
}

func (tx *cacheTransaction) close() {
	if tx.configRoot != nil {
		_ = tx.configRoot.Close()
	}
	if tx.dataRoot != nil {
		_ = tx.dataRoot.Close()
	}
}

func (tx *cacheTransaction) root(kind cacheRootKind) (securefs.TrustedRoot, error) {
	if kind == cacheConfigRoot {
		if tx.configRoot == nil {
			root, err := tx.cache.openTrustedRoot(tx.cache.configPath)
			if err != nil {
				return nil, err
			}
			tx.configRoot = root
		}
		return tx.configRoot, nil
	}
	if tx.dataRoot == nil {
		root, err := tx.cache.openTrustedRoot(tx.cache.dataPath)
		if err != nil {
			return nil, err
		}
		tx.dataRoot = root
	}
	return tx.dataRoot, nil
}

func locationID(location cacheLocation) string {
	return fmt.Sprintf("%d\x00%s", location.root, strings.Join(location.segments, "\x00"))
}

func (tx *cacheTransaction) snapshot(location cacheLocation) error {
	id := locationID(location)
	if _, ok := tx.snapshots[id]; ok {
		return nil
	}
	root, err := tx.root(location.root)
	if err != nil {
		return err
	}
	data, err := root.Read(location.segments...)
	snapshot := cacheSnapshot{location: location}
	switch {
	case err == nil:
		snapshot.data = data
		snapshot.exists = true
	case errors.Is(err, os.ErrNotExist):
	default:
		return err
	}
	tx.snapshots[id] = snapshot
	tx.order = append(tx.order, id)
	return nil
}

func (tx *cacheTransaction) ensureParent(location cacheLocation) error {
	if len(location.segments) <= 1 {
		return nil
	}
	root, err := tx.root(location.root)
	if err != nil {
		return err
	}
	return root.EnsureDir(location.segments[:len(location.segments)-1], 0o700)
}

func (tx *cacheTransaction) write(location cacheLocation, data []byte) error {
	if err := tx.snapshot(location); err != nil {
		return err
	}
	if err := tx.ensureParent(location); err != nil {
		return err
	}
	root, err := tx.root(location.root)
	if err != nil {
		return err
	}
	return root.AtomicWrite(location.segments, data, 0o600)
}

func (tx *cacheTransaction) remove(location cacheLocation) error {
	if err := tx.snapshot(location); err != nil {
		return err
	}
	root, err := tx.root(location.root)
	if err != nil {
		return err
	}
	if err := root.Remove(location.segments...); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (tx *cacheTransaction) apply(entry Entry) error {
	location, err := tx.cache.entryLocation(entry.Kind, entry.Grp, entry.Key)
	if err != nil {
		return err
	}
	if entry.Deleted {
		return tx.remove(location)
	}
	return tx.write(location, entry.Ciphertext)
}

func (tx *cacheTransaction) rollback() error {
	var rollbackErr error
	for index := len(tx.order) - 1; index >= 0; index-- {
		snapshot := tx.snapshots[tx.order[index]]
		identity := strings.Join(snapshot.location.segments, "/")
		root, err := tx.root(snapshot.location.root)
		if err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("open rollback root for %q: %w", identity, err))
			continue
		}
		if snapshot.exists {
			if err := tx.ensureParent(snapshot.location); err != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("prepare rollback parent for %q: %w", identity, err))
				continue
			}
			if err := root.AtomicWrite(snapshot.location.segments, snapshot.data, 0o600); err != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore rollback entry %q: %w", identity, err))
			}
			continue
		}
		if err := root.Remove(snapshot.location.segments...); err != nil && !errors.Is(err, os.ErrNotExist) {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove rollback entry %q: %w", identity, err))
		}
	}
	return rollbackErr
}

func (c *localCache) mutate(fn func(*cacheTransaction) error) error {
	tx := newCacheTransaction(c)
	defer tx.close()
	if err := fn(tx); err != nil {
		if rollbackErr := tx.rollback(); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("cache rollback failure: %w", rollbackErr))
		}
		return err
	}
	return nil
}

func stateBytes(state *syncState) ([]byte, error) {
	return json.MarshalIndent(state, "", "  ")
}

// StateRegressionError 表示待写入状态相对磁盘现状出现退化（快照丢失），写入被拒绝。
type StateRegressionError struct {
	LostEntries     int
	MetadataCleared bool
}

func (e *StateRegressionError) Error() string {
	switch {
	case e.LostEntries > 0:
		return fmt.Sprintf("同步状态防退化校验拒绝写入：将丢失 %d 个条目快照且无对应删除标记；如两端数据一致可执行 senv sync --accept-remote 重建", e.LostEntries)
	default:
		return "同步状态防退化校验拒绝写入：metadata_hash 将从非空变为空串；如两端数据一致可执行 senv sync --accept-remote 重建"
	}
}

// stateWriteOptions 控制一次状态写入的来源与护栏白名单。
type stateWriteOptions struct {
	writerPath string
	// allowEntryShrink 用于 bootstrap/accept-remote/migrate 等显式全量重建路径。
	allowEntryShrink bool
	// allowEmptyMetadata 仅允许 acceptRemote 在远端确无 metadata 时写入空哈希。
	allowEmptyMetadata bool
	// removedEntries 是本次写入合法消失的条目（已推送/应用的 tombstone）。
	removedEntries map[string]bool
}

// encodeStateChecked 是所有状态落盘的统一咽喉：先对照磁盘现状做退化校验，
// 再补全 vault 绑定与写入来源，最后序列化。绑定/来源字段不含敏感内容。
func (c *localCache) encodeStateChecked(state *syncState, opts stateWriteOptions) ([]byte, error) {
	if existing, ok, err := c.readStateRaw(); err != nil {
		return nil, err
	} else if ok {
		if err := validateNoStateRegression(existing, state, opts); err != nil {
			return nil, err
		}
	}
	stamped := *state
	if c.binding != nil {
		binding := *c.binding
		stamped.VaultBinding = &binding
	}
	stamped.WrittenBy = &stateWriterInfo{Path: opts.writerPath, PID: os.Getpid(), TS: time.Now().Unix()}
	return stateBytes(&stamped)
}

// validateNoStateRegression 拒绝两类退化：无 tombstone 的快照净减少、metadata 哈希非空→空。
func validateNoStateRegression(existing, next *syncState, opts stateWriteOptions) error {
	if !opts.allowEntryShrink {
		var lost int
		for id := range existing.Entries {
			if _, ok := next.Entries[id]; !ok && !opts.removedEntries[id] {
				lost++
			}
		}
		if lost > 0 {
			return &StateRegressionError{LostEntries: lost}
		}
	}
	if existing.MetadataHash != "" && next.MetadataHash == "" && !opts.allowEmptyMetadata {
		return &StateRegressionError{MetadataCleared: true}
	}
	return nil
}

// applyRemote commits a validated pull as one recoverable in-process batch.
// Sync state is written last and every synchronous failure restores snapshots.
func (c *localCache) applyRemote(entries []Entry, metadata []byte, updateMetadata bool, state *syncState) error {
	return c.applyRemoteOpts(entries, metadata, updateMetadata, state, stateWriteOptions{writerPath: "applyRemote"})
}

func (c *localCache) applyRemoteOpts(entries []Entry, metadata []byte, updateMetadata bool, state *syncState, opts stateWriteOptions) error {
	if err := validateRemoteEntries(entries); err != nil {
		return err
	}
	if updateMetadata {
		if _, err := storage.ParseMetadata(metadata); err != nil {
			return fmt.Errorf("invalid synced metadata: %w", err)
		}
	}
	encodedState, err := c.encodeStateChecked(state, opts)
	if err != nil {
		return err
	}
	return c.mutate(func(tx *cacheTransaction) error {
		for _, entry := range entries {
			if err := tx.apply(entry); err != nil {
				return err
			}
		}
		if updateMetadata {
			if err := tx.write(cacheLocation{root: cacheConfigRoot, segments: []string{storage.MetadataFile}}, metadata); err != nil {
				return err
			}
		}
		return tx.write(cacheLocation{root: cacheDataRoot, segments: []string{syncStateFileName}}, encodedState)
	})
}

func (c *localCache) apply(entry Entry) error {
	if err := validateRemoteEntries([]Entry{entry}); err != nil {
		return err
	}
	return c.mutate(func(tx *cacheTransaction) error { return tx.apply(entry) })
}

func (c *localCache) readMetadata() ([]byte, error) {
	root, err := c.openExistingRoot(c.configPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	data, err := root.Read(storage.MetadataFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

func (c *localCache) writeMetadata(blob []byte) error {
	if _, err := storage.ParseMetadata(blob); err != nil {
		return fmt.Errorf("invalid synced metadata: %w", err)
	}
	return c.mutate(func(tx *cacheTransaction) error {
		return tx.write(cacheLocation{root: cacheConfigRoot, segments: []string{storage.MetadataFile}}, blob)
	})
}

func (c *localCache) loadState() (*syncState, error) {
	state, ok, err := c.readStateRaw()
	if err != nil {
		return nil, err
	}
	if !ok {
		return newSyncState(), nil
	}
	// vault 绑定校验：状态文件属于其他 vault 时拒绝复用，防止交叉污染快照。
	if state.VaultBinding != nil && c.binding != nil && *state.VaultBinding != *c.binding {
		return nil, fmt.Errorf(
			"同步状态文件绑定到其他 vault（server=%s vault=%s，当前 server=%s vault=%s）；如确认切换请执行 senv sync --accept-remote 重建",
			state.VaultBinding.Server, state.VaultBinding.Vault, c.binding.Server, c.binding.Vault,
		)
	}
	return state, nil
}

// readStateRaw 读取并解码状态文件，不做 vault 绑定校验；文件不存在时 ok=false。
func (c *localCache) readStateRaw() (*syncState, bool, error) {
	root, err := c.openExistingRoot(c.dataPath)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()
	data, err := root.Read(syncStateFileName)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var state syncState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, false, fmt.Errorf("同步状态文件损坏（%s）: %w；删除该文件后执行 senv init 或 senv sync 可重建", c.stateFilePath(), err)
	}
	if state.Entries == nil {
		state.Entries = make(map[string]syncEntryState)
	}
	return &state, true, nil
}

func (c *localCache) saveState(state *syncState) error {
	return c.saveStateOpts(state, stateWriteOptions{writerPath: "saveState"})
}

func (c *localCache) saveStateOpts(state *syncState, opts stateWriteOptions) error {
	data, err := c.encodeStateChecked(state, opts)
	if err != nil {
		return err
	}
	return c.mutate(func(tx *cacheTransaction) error {
		return tx.write(cacheLocation{root: cacheDataRoot, segments: []string{syncStateFileName}}, data)
	})
}
