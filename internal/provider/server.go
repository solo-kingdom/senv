package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wii/senv/internal/perflog"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
	"github.com/wii/senv/internal/syncschema"
)

// defaultPushBatchSize 是单次 push 请求携带的条目上限；服务端 MaxBatchEntries 为 1000。
// 条目总数过千（rekey、批量导入、台账记录增长）时必须分批，否则整批被拒。
const defaultPushBatchSize = 500

// pushBatchSize 返回本次推送的批大小；SENV_PUSH_BATCH 可覆盖（1..1000，非法值回退默认）。
func pushBatchSize() int {
	if v := strings.TrimSpace(os.Getenv("SENV_PUSH_BATCH")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			return n
		}
	}
	return defaultPushBatchSize
}

// ServerProvider 是 server provider：本地缓存（复用 storage.Manager 文件格式）为工作副本，
// senv-server 为远端。同步 = 增量 pull 落盘 + 收集 dirty 条目乐观锁批量 push。
// 所有同步入口（Sync/AutoPull/…）在 dataPath 同步锁内串行执行；now 可注入以便测试节流。
type ServerProvider struct {
	api          serverAPI
	cache        *localCache
	vault        string
	now          func() time.Time
	autoSync     bool
	syncThrottle time.Duration
}

// newServerProvider 构造 server provider（接口实现），api 可注入以便测试
func newServerProvider(api serverAPI, configPath, dataPath, vault string) *ServerProvider {
	cache := &localCache{configPath: configPath, dataPath: dataPath}
	if client, ok := api.(*serverClient); ok {
		// 生产路径按 server 地址指纹 + vault 名绑定状态文件。
		cache.binding = &vaultBinding{Server: serverFingerprint(client.baseURL), Vault: vault}
	}
	return &ServerProvider{
		api:          api,
		cache:        cache,
		vault:        vault,
		now:          time.Now,
		autoSync:     true,
		syncThrottle: DefaultSyncThrottle,
	}
}

// NewServerProvider 以 HTTP client 构造 server provider（CLI 使用）。
// server 判定本 client 被屏蔽（403 client_blocked）时，首次响应会触发本地
// 解锁缓存清理（加密工作副本保留），此后每次请求仍返回 ErrClientBlocked。
func NewServerProvider(address, token, configPath, dataPath, vault string) *ServerProvider {
	client := newServerClient(address, token)
	p := newServerProvider(client, configPath, dataPath, vault)
	client.onBlocked = p.clearLocalSession
	return p
}

// clearLocalSession 清空本机解锁缓存并写入审计事件；由 serverClient 在收到
// 屏蔽响应时回调（每实例至多一次）。清理失败不阻断错误返回——解锁缓存残留
// 时用户可手动 `senv session clear`，本地加密数据始终不动。
func (p *ServerProvider) clearLocalSession() {
	mgr := session.NewManager(p.cache.configPath, p.cache.dataPath)
	defer mgr.Close()
	// 清理失败不影响屏蔽错误主路径；解锁缓存残留时用户可手动 senv session clear
	_ = mgr.ClearSession()
	if al := mgr.GetAuditLogger(); al != nil {
		_ = al.Log(session.AuditClientBlocked, "", false, "server marked this client blocked; local session cache cleared")
	}
}

// newServerProviderWithBinding 是测试专用构造：注入合成 vault 绑定验证归属校验。
func newServerProviderWithBinding(api serverAPI, configPath, dataPath, vault string, binding vaultBinding) *ServerProvider {
	p := newServerProvider(api, configPath, dataPath, vault)
	b := binding
	p.cache.binding = &b
	return p
}

// ErrHistoryUnsupported 表示当前 server API 不支持条目历史查询
var ErrHistoryUnsupported = errors.New("当前 server 不支持条目历史查询（请升级 senv-server）")

// historyAPI 由支持历史查询的 server client 实现；独立窄接口使旧测试 fake 无感
type historyAPI interface {
	History(ctx context.Context, vault string, f HistoryFilter) ([]HistoryVersion, error)
	MetadataHistory(ctx context.Context, vault string, limit int) ([]MetadataVersion, error)
}

// MetadataVersion 是 server 端保留的一份 vault metadata 历史（不透明 blob）
type MetadataVersion struct {
	Blob       []byte    `json:"blob"`
	ReplacedAt time.Time `json:"replaced_at"`
}

// MetadataHistory 查询 vault metadata 的历史版本（只读）
func (p *ServerProvider) MetadataHistory(ctx context.Context, limit int) ([]MetadataVersion, error) {
	api, ok := p.api.(historyAPI)
	if !ok {
		return nil, ErrHistoryUnsupported
	}
	return api.MetadataHistory(ctx, p.vault, limit)
}

// RestoreMetadata 把一份历史 metadata 恢复为当前值：先备份本地当前文件 → 写回历史
// blob → 带远端当前 hash 推送（不绕过乐观锁）。推送失败时回滚本地文件。
// metadata 决定 vault 能否解锁，恢复前必须由调用方向用户确认。
func (p *ServerProvider) RestoreMetadata(ctx context.Context, blob []byte) error {
	release, err := p.lockBlocking()
	if err != nil {
		return err
	}
	defer release()

	remote, err := p.api.GetMetadata(ctx, p.vault)
	base := ""
	switch {
	case err == nil:
		base = hashBytes(remote)
	case errors.Is(err, ErrVaultNotFound):
	default:
		return err
	}

	local, err := p.cache.readMetadata()
	if err != nil {
		return err
	}
	if err := p.cache.writeMetadata(blob); err != nil {
		return err
	}
	if err := p.api.PutMetadataIfMatch(ctx, p.vault, blob, base); err != nil {
		if local != nil {
			if rbErr := p.cache.writeMetadata(local); rbErr != nil {
				return fmt.Errorf("%w（本地 metadata 回滚失败: %v）", err, rbErr)
			}
		}
		return err
	}
	st, err := p.cache.loadState()
	if err != nil {
		return err
	}
	st.MetadataHash = hashBytes(blob)
	return p.cache.saveStateOpts(st, stateWriteOptions{writerPath: "restoreMetadata"})
}

// History 查询条目历史（只读，不改本地缓存与同步状态）
func (p *ServerProvider) History(ctx context.Context, f HistoryFilter) ([]HistoryVersion, error) {
	api, ok := p.api.(historyAPI)
	if !ok {
		return nil, ErrHistoryUnsupported
	}
	return api.History(ctx, p.vault, f)
}

// RestoreEntry 把历史版本恢复为当前值：历史密文写回本地缓存 → 走既有乐观锁
// 推送产生新 revision（不绕过冲突检测）。已删除/不存在条目的恢复等价于以
// 历史密文重新创建。推送失败时本地恢复已生效（快照未推进），后续 sync 会
// 把它当作本地改动重新推送。
func (p *ServerProvider) lookupEntriesOrPull(ctx context.Context, keys []EntryKey, metaOnly bool) ([]Entry, int64, error) {
	if len(keys) == 0 {
		return []Entry{}, 0, nil
	}
	if api, ok := p.api.(lookupAPI); ok {
		var all []Entry
		var latest int64
		supported := true
		for start := 0; start < len(keys); start += 1000 {
			end := start + 1000
			if end > len(keys) {
				end = len(keys)
			}
			got, rev, err := api.Lookup(ctx, p.vault, keys[start:end], metaOnly)
			if err != nil {
				if errors.Is(err, ErrLookupUnsupported) {
					supported = false
					break
				}
				return nil, 0, err
			}
			all = append(all, got...)
			latest = rev
		}
		if supported {
			return all, latest, nil
		}
	}
	return p.api.Pull(ctx, p.vault, 0)
}

func (p *ServerProvider) pullAllEntries(ctx context.Context, includeDeleted bool) ([]Entry, int64, error) {
	if api, ok := p.api.(pullWithMetaAPI); ok {
		entries, latest, _, err := api.PullWithMeta(ctx, p.vault, 0, includeDeleted)
		return entries, latest, err
	}
	return p.api.Pull(ctx, p.vault, 0)
}

func (p *ServerProvider) RestoreEntry(ctx context.Context, kind, grp, key string, ciphertext []byte) error {
	if err := syncschema.ValidateIdentity(kind, grp, key); err != nil {
		return fmt.Errorf("无效的条目标识: %w", err)
	}
	release, err := p.lockBlocking()
	if err != nil {
		return err
	}
	defer release()

	// 阶段 1（无 vault 锁）：查询目标条目当前 revision
	remote, _, err := p.lookupEntriesOrPull(ctx, []EntryKey{{Kind: kind, Grp: grp, Key: key}}, true)
	if err != nil {
		return err
	}
	if err := validateRemoteEntries(remote); err != nil {
		return err
	}
	remoteRevision := int64(0)
	if e, ok := entryMap(remote)[entryID(kind, grp, key)]; ok {
		remoteRevision = e.Revision
	}

	// 阶段 2（短写锁）：本地写回历史密文
	if err := p.withVaultMutation(func() error {
		st, err := p.cache.loadState()
		if err != nil {
			return err
		}
		return p.cache.applyRemote([]Entry{{Kind: kind, Grp: grp, Key: key, Ciphertext: ciphertext}}, nil, false, st)
	}); err != nil {
		return err
	}

	// 阶段 3（无 vault 锁）：推送到 server
	pushes := []Entry{{Kind: kind, Grp: grp, Key: key, Ciphertext: ciphertext, BaseRevision: remoteRevision}}
	pushed, latest, err := p.api.Push(ctx, p.vault, pushes)
	if err != nil {
		var conflictErr *ConflictError
		if errors.As(err, &conflictErr) {
			var localMeta []byte
			_ = p.withVaultMutation(func() error {
				var metaErr error
				localMeta, metaErr = p.cache.readMetadata()
				return metaErr
			})
			remoteMeta, metaErr := p.api.GetMetadata(ctx, p.vault)
			if metaErr != nil && !errors.Is(metaErr, ErrVaultNotFound) {
				return metaErr
			}
			return p.buildConflictError(ctx, conflictErr.Conflicts, false, pushes, remote, localMeta, remoteMeta)
		}
		return err
	}

	// 阶段 4（短写锁）：更新同步状态
	return p.withVaultMutation(func() error {
		st, err := p.cache.loadState()
		if err != nil {
			return err
		}
		st.LastSyncedRevision = latest
		for _, entry := range pushed {
			st.Entries[entryID(entry.Kind, entry.Grp, entry.Key)] = syncEntryState{
				Revision: entry.Revision, Hash: hashBytes(entry.Ciphertext),
			}
		}
		return p.cache.saveState(st)
	})
}

// SyncConflictError 同步因乐观锁冲突中止；两端数据均未改动。
// 附解决指引：accept-remote（放弃本地）或 force-push（放弃远端）。
type SyncConflictError struct {
	Conflicts        []Conflict
	MetadataConflict bool
	Details          []ConflictDetail
	Metadata         *MetadataConflictDetail
}

// ConflictSide 描述冲突一侧的非机密信息。Ciphertext 仅供已认证渲染和
// resolution 流程使用，绝不能进入 Error() 或日志。
type ConflictSide struct {
	Revision   int64     `json:"revision"`
	Deleted    bool      `json:"deleted"`
	Size       int       `json:"size"`
	Hash       string    `json:"hash"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
	Ciphertext []byte    `json:"-"`
}

// ConflictDetail 将本地待推送版本与远端当前版本配对，供 CLI 安全渲染。
type ConflictDetail struct {
	Kind   string       `json:"kind"`
	Grp    string       `json:"grp"`
	Key    string       `json:"key"`
	Local  ConflictSide `json:"local"`
	Remote ConflictSide `json:"remote"`
}

// MetadataConflictDetail 保存两端 metadata blob 供 key 兼容性诊断；
// raw blob 不进入用户可见错误文本。
type MetadataConflictDetail struct {
	Local  []byte `json:"-"`
	Remote []byte `json:"-"`
}

func (e *SyncConflictError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "同步中止：%d 个条目在远端已被更新（两端数据均未改动）\n冲突条目:", len(e.Conflicts))
	for _, c := range e.Conflicts {
		fmt.Fprintf(&b, "\n  - %s/%s/%s (远端 revision: %d)", displayKind(c.Kind), displayGrp(c.Grp, c.Key), displayKey(c.Kind, c.Key), c.CurrentRevision)
	}
	if e.MetadataConflict {
		b.WriteString("\n  - vault metadata（本地与远端均已修改）")
	}
	b.WriteString("\n解决方式（二选一）:")
	b.WriteString("\n  senv sync --accept-remote  放弃本地改动，以远端为准")
	b.WriteString("\n  senv sync --force-push     放弃远端改动，以本地为准")
	return b.String()
}

func displayKind(kind string) string { return kind }

func displayGrp(grp, key string) string {
	if grp == "" {
		return "-"
	}
	return grp
}

func displayKey(kind, key string) string {
	if key == "" {
		return "(index/meta)"
	}
	return key
}

func entryMap(entries []Entry) map[string]Entry {
	m := make(map[string]Entry, len(entries))
	for _, e := range entries {
		m[entryID(e.Kind, e.Grp, e.Key)] = e
	}
	return m
}

func localConflictSide(e Entry) ConflictSide {
	return ConflictSide{
		Revision: e.BaseRevision, Deleted: e.Deleted, Size: len(e.Ciphertext),
		Hash: hashBytes(e.Ciphertext), Ciphertext: e.Ciphertext,
	}
}

func remoteConflictSide(c Conflict, candidate Entry, hasCandidate bool) ConflictSide {
	side := ConflictSide{
		Revision: c.CurrentRevision, Deleted: c.Deleted, Size: int(c.Size),
		UpdatedAt: c.UpdatedAt,
	}
	// Legacy servers only return identity/current_revision. A matching pull
	// candidate can safely fill the descriptor without another network call.
	legacyDescriptor := c.Size == 0 && c.UpdatedAt.IsZero()
	if hasCandidate && candidate.Revision == c.CurrentRevision {
		side.Deleted = candidate.Deleted || (legacyDescriptor && candidate.Deleted)
		if legacyDescriptor && !side.Deleted {
			side.Size = len(candidate.Ciphertext)
		}
		if c.UpdatedAt.IsZero() {
			side.UpdatedAt = candidate.UpdatedAt
		}
		side.Hash = hashBytes(candidate.Ciphertext)
		side.Ciphertext = candidate.Ciphertext
	}
	return side
}

// buildConflictError pairs push candidates with the remote versions that were
// skipped during pull. If the remote moved again between pull and push, one
// full pull refreshes the stale candidate; refresh failure retains descriptor-
// only details rather than showing known-stale ciphertext.
func (p *ServerProvider) buildConflictError(
	ctx context.Context, conflicts []Conflict, metadataConflict bool,
	dirty, candidates []Entry, localMeta, remoteMeta []byte,
) *SyncConflictError {
	known := entryMap(candidates)
	var missingKeys []EntryKey
	for _, c := range conflicts {
		candidate, ok := known[entryID(c.Kind, c.Grp, c.Key)]
		if !ok || candidate.Revision != c.CurrentRevision {
			missingKeys = append(missingKeys, EntryKey{Kind: c.Kind, Grp: c.Grp, Key: c.Key})
		}
	}
	if len(missingKeys) > 0 {
		if entries, _, err := p.lookupEntriesOrPull(ctx, missingKeys, false); err == nil {
			if err := validateRemoteEntries(entries); err == nil {
				for _, e := range entries {
					known[entryID(e.Kind, e.Grp, e.Key)] = e
				}
			}
		}
	}

	dirtyByID := entryMap(dirty)
	details := make([]ConflictDetail, 0, len(conflicts))
	for _, c := range conflicts {
		id := entryID(c.Kind, c.Grp, c.Key)
		local, hasLocal := dirtyByID[id]
		candidate, hasCandidate := known[id]
		detail := ConflictDetail{
			Kind: c.Kind, Grp: c.Grp, Key: c.Key,
			Remote: remoteConflictSide(c, candidate, hasCandidate),
		}
		if hasLocal {
			detail.Local = localConflictSide(local)
		}
		details = append(details, detail)
	}

	out := &SyncConflictError{
		Conflicts: conflicts, MetadataConflict: metadataConflict, Details: details,
	}
	if metadataConflict {
		out.Metadata = &MetadataConflictDetail{Local: localMeta, Remote: remoteMeta}
	}
	return out
}

// lockBlocking 获取阻塞式同步锁（手动同步入口：锁忙时等待而非跳过）。
func (p *ServerProvider) lockBlocking() (func(), error) {
	lock, err := acquireSyncLock(p.cache.dataPath, true)
	if err != nil {
		return nil, err
	}
	return func() { _ = lock.release() }, nil
}

func (p *ServerProvider) withVaultMutation(fn func() error) error {
	manager := storage.NewManager(p.cache.configPath, p.cache.dataPath)
	return manager.WithVaultMutation(func(*storage.Manager) error { return fn() })
}

// Bootstrap 初始化本地缓存：拉取 metadata 与全部条目落盘，建立同步状态。
// vault 在 server 端不存在时返回 ErrVaultNotFound。
func (p *ServerProvider) Bootstrap(ctx context.Context) error {
	release, err := p.lockBlocking()
	if err != nil {
		return err
	}
	defer release()
	return p.bootstrapLocked(ctx)
}

func (p *ServerProvider) bootstrapLocked(ctx context.Context) error {
	blob, err := p.api.GetMetadata(ctx, p.vault)
	if err != nil {
		return err
	}
	entries, latest, err := p.pullAllEntries(ctx, false)
	if err != nil {
		return err
	}
	if err := validateRemoteEntries(entries); err != nil {
		return err
	}
	return p.withVaultMutation(func() error {
		st := newSyncState()
		st.MetadataHash = hashBytes(blob)
		st.LastSyncedRevision = latest
		st.LastPullAt = p.now().Unix()
		for _, e := range entries {
			if e.Deleted {
				continue
			}
			st.Entries[entryID(e.Kind, e.Grp, e.Key)] = syncEntryState{Revision: e.Revision, Hash: hashBytes(e.Ciphertext)}
		}
		return p.cache.applyRemoteOpts(entries, blob, true, st, stateWriteOptions{
			writerPath:       "bootstrapLocked",
			allowEntryShrink: true,
		})
	})
}

// collectDirty 对比快照与当前本地缓存，返回待推送条目（含删除标记）。
// base_revision 取快照中记录的 server revision（本地新增为 0）。
func (p *ServerProvider) collectDirty(st *syncState, current map[string]Entry) []Entry {
	var dirty []Entry
	for id, cur := range current {
		snap, ok := st.Entries[id]
		if !ok {
			cur.BaseRevision = 0
			dirty = append(dirty, cur)
			continue
		}
		if snap.Hash != hashBytes(cur.Ciphertext) {
			cur.BaseRevision = snap.Revision
			dirty = append(dirty, cur)
		}
	}
	for id, snap := range st.Entries {
		if _, ok := current[id]; ok {
			continue
		}
		// 本地已删除：推送删除标记
		parts := strings.SplitN(id, "\x00", 3)
		dirty = append(dirty, Entry{Kind: parts[0], Grp: parts[1], Key: parts[2], BaseRevision: snap.Revision, Deleted: true})
	}
	return dirty
}

// dirtyIDs 返回 dirty 条目 id 集合（pull 落盘时保护本地未推送改动）
func dirtyIDs(dirty []Entry) map[string]bool {
	ids := make(map[string]bool, len(dirty))
	for _, e := range dirty {
		ids[entryID(e.Kind, e.Grp, e.Key)] = true
	}
	return ids
}

// PullResult 汇总一次 pull 的结果
type PullResult struct {
	Applied          int // 落盘的条目数
	SkippedDirty     int // 因本地有未推送改动而跳过的条目数
	RemoteCandidates []Entry
	MetadataUpdated  bool // metadata 是否被远端版本更新
	MetadataHealed   bool // 假冲突自愈：快照哈希失真但两端字节一致
	MetadataConflict bool // metadata 两端均已修改
	LatestRevision   int64
}

// pull 增量拉取并落盘，拆为三段执行（网络阶段移出 vault 锁，见 change
// tui-startup-perf 的 D1）：
//  1. 读锁收集本地 manifest（同步状态：增量游标 + 条目指纹快照）；
//  2. 无锁执行 api.Pull/GetMetadata 网络请求——此阶段不持有 vault 锁，
//     TUI 等本地读取可与慢网络并行，不再在锁上排队；
//  3. 写锁应用响应落盘（单事务原子写，失败回滚）。
//
// 调用方仍全程持有同步锁（.senv-sync.lock）：throttle/push 串行语义不变，
// 阶段 3 的状态基线不会被并发 sync 改写。本地 dirty 的条目不被远端覆盖
// （留给 push 乐观锁判定）——语义与旧单锁实现一致。
func (p *ServerProvider) pull(ctx context.Context) (*PullResult, error) {
	return p.pullWithOpts(ctx, true)
}

func (p *ServerProvider) pullWithOpts(ctx context.Context, allowMetadataKeyChange bool) (*PullResult, error) {
	manifest, expectedMetaHash, err := p.pullManifest()
	if err != nil {
		return nil, err
	}
	remote, latest, remoteMeta, metaUnchanged, vaultFound, err := p.pullFetch(ctx, manifest.LastSyncedRevision, expectedMetaHash)
	if err != nil {
		return nil, err
	}
	return p.pullApply(manifest, remote, latest, remoteMeta, metaUnchanged, expectedMetaHash, vaultFound, allowMetadataKeyChange)
}

// pullManifest 是 pull 阶段 1：短暂持有 vault 锁读取同步状态（版本/指纹），
// LastSyncedRevision 作为增量请求游标，状态本体作为阶段 3 的推进基线。
func (p *ServerProvider) pullManifest() (*syncState, string, error) {
	t := perflog.Start("sync.pull-manifest")
	var st *syncState
	var localMetaHash string
	err := p.withVaultMutation(func() error {
		var inner error
		st, inner = p.cache.loadState()
		if inner != nil {
			return inner
		}
		if localMeta, mErr := p.cache.readMetadata(); mErr == nil && len(localMeta) > 0 {
			localMetaHash = hashBytes(localMeta)
		}
		return nil
	})
	t.End(err == nil)
	if err != nil {
		return nil, "", err
	}
	return st, localMetaHash, nil
}

// pullFetch 是 pull 阶段 2：不持有 vault 锁执行 api.Pull 与 api.GetMetadata，
// 网络等待与本地 vault 读取/写入并行。返回的远端条目已在任何落盘前完成
// 身份校验（含将被 dirty 跳过的条目）。
func (p *ServerProvider) pullFetch(ctx context.Context, since int64, expectedMetaHash string) (entries []Entry, latest int64, remoteMeta []byte, metaUnchanged bool, vaultFound bool, err error) {
	t := perflog.Start("sync.pull-net")
	defer func() { t.End(err == nil) }()
	vaultFound = true
	var remoteMetaHash string
	if api, ok := p.api.(pullWithMetaAPI); ok {
		entries, latest, remoteMetaHash, err = api.PullWithMeta(ctx, p.vault, since, true)
	} else {
		entries, latest, err = p.api.Pull(ctx, p.vault, since)
	}
	if errors.Is(err, ErrVaultNotFound) {
		entries, latest, err = nil, 0, nil
		vaultFound = false
	}
	if err != nil {
		return nil, 0, nil, false, false, err
	}
	if err := validateRemoteEntries(entries); err != nil {
		return nil, 0, nil, false, false, err
	}
	if remoteMetaHash != "" && expectedMetaHash != "" && remoteMetaHash == expectedMetaHash {
		return entries, latest, nil, true, vaultFound, nil
	}
	remoteMeta, err = p.api.GetMetadata(ctx, p.vault)
	if err != nil {
		if errors.Is(err, ErrVaultNotFound) {
			vaultFound = false
			return entries, latest, nil, false, false, nil
		}
		return nil, 0, nil, false, false, err
	}
	return entries, latest, remoteMeta, false, vaultFound, nil
}

// pullApply 是 pull 阶段 3：持有 vault 写锁把远端响应应用落盘。单事务写入
// 条目 + metadata + 状态文件，任何同步失败整体回滚。
func (p *ServerProvider) pullApply(st *syncState, remote []Entry, latest int64, remoteMeta []byte, metaUnchanged bool, expectedMetaHash string, vaultFound, allowMetadataKeyChange bool) (*PullResult, error) {
	t := perflog.Start("sync.pull-apply")
	var res *PullResult
	err := p.withVaultMutation(func() error {
		var inner error
		res, inner = p.pullApplyLocked(st, remote, latest, remoteMeta, metaUnchanged, expectedMetaHash, vaultFound, allowMetadataKeyChange)
		return inner
	})
	t.End(err == nil)
	return res, err
}

// pullApplyLocked 在写锁内重算 dirty 并应用远端条目。阶段 2 不持锁，本地写
// 可能已落入——这里重新对比快照与现状，dirty 条目（含网络窗口内新增的写）
// 跳过不覆盖，保持 dirty 队列语义不变，交给后续 push 乐观锁判定。
func (p *ServerProvider) pullApplyLocked(st *syncState, remote []Entry, latest int64, remoteMeta []byte, metaUnchanged bool, expectedMetaHash string, vaultFound, allowMetadataKeyChange bool) (*PullResult, error) {
	if vaultFound && st.LastSyncedRevision > 0 && latest < st.LastSyncedRevision {
		return nil, fmt.Errorf("检测到远端同步版本号回滚（local=%d, remote=%d），已拒绝拉取", st.LastSyncedRevision, latest)
	}
	current, err := p.cache.collect()
	if err != nil {
		return nil, err
	}
	dirty := dirtyIDs(p.collectDirty(st, current))
	res := &PullResult{LatestRevision: latest}
	toApply := make([]Entry, 0, len(remote))
	// 本次合法消失的条目（远端 tombstone），供状态防退化护栏放行。
	removed := make(map[string]bool)
	for _, e := range remote {
		id := entryID(e.Kind, e.Grp, e.Key)
		if dirty[id] {
			res.SkippedDirty++
			res.RemoteCandidates = append(res.RemoteCandidates, e)
			continue
		}
		if prev, hasPrev := st.Entries[id]; hasPrev {
			if e.Revision < prev.Revision {
				return nil, fmt.Errorf("检测到远端条目 %s/%s/%s 版本回滚（local=%d, remote=%d），已拒绝拉取", e.Kind, e.Grp, e.Key, prev.Revision, e.Revision)
			}
			if e.Revision == prev.Revision && !e.Deleted && prev.Hash != "" && hashBytes(e.Ciphertext) != prev.Hash {
				return nil, fmt.Errorf("检测到远端条目 %s/%s/%s 同版本密文被篡改（revision=%d），已拒绝拉取", e.Kind, e.Grp, e.Key, e.Revision)
			}
		}
		toApply = append(toApply, e)
		res.Applied++
		if e.Deleted {
			delete(st.Entries, id)
			removed[id] = true
		} else {
			st.Entries[id] = syncEntryState{Revision: e.Revision, Hash: hashBytes(e.Ciphertext)}
		}
	}

	localMeta, err := p.cache.readMetadata()
	if err != nil {
		return nil, err
	}
	if localParsed, perr := storage.ParseMetadata(localMeta); perr == nil {
		if iter, ierr := localParsed.ValidatedKDFIterations(); ierr == nil && iter > st.MinKDFIterations {
			st.MinKDFIterations = iter
		}
	}
	localHash := hashBytes(localMeta)
	remoteHash := localHash
	if !metaUnchanged || localHash != expectedMetaHash {
		remoteHash = hashBytes(remoteMeta)
	}
	updateMetadata := false
	if metaUnchanged && localHash == expectedMetaHash {
		if st.MetadataHash != localHash {
			st.MetadataHash = localHash
			res.MetadataHealed = true
		}
	} else if remoteHash != localHash {
		// 能解析的远端 metadata 才做防降级与 salt 变更判定；不可解析的保持旧语义
		// （更新时由 applyRemoteOpts 校验并拒绝）。
		remoteIter := 0
		keyChanged := false
		if remoteParsed, perr := storage.ParseMetadata(remoteMeta); perr == nil {
			iter, ierr := remoteParsed.ValidatedKDFIterations()
			if ierr != nil {
				return nil, fmt.Errorf("invalid remote metadata KDF: %w", ierr)
			}
			remoteIter = iter
			if st.MinKDFIterations > 0 && remoteIter < st.MinKDFIterations {
				return nil, fmt.Errorf("远端 metadata 试图将 KDF 迭代次数从 %d 降级至 %d，已拒绝", st.MinKDFIterations, remoteIter)
			}
			if localParsed, lerr := storage.ParseMetadata(localMeta); lerr == nil {
				keyChanged = localParsed.Salt != remoteParsed.Salt || localParsed.PasswordKey != remoteParsed.PasswordKey
			}
		}
		if localHash == st.MetadataHash && (allowMetadataKeyChange || !keyChanged) {
			st.MetadataHash = remoteHash
			if remoteIter > st.MinKDFIterations {
				st.MinKDFIterations = remoteIter
			}
			res.MetadataUpdated = true
			updateMetadata = true
		} else {
			res.MetadataConflict = true
		}
	} else if st.MetadataHash != localHash {
		// 假冲突自愈：两端 metadata 字节一致，仅本地快照哈希失真（如历史状态损坏）。
		// 不写 metadata 文件、不算冲突，直接收养哈希。
		st.MetadataHash = localHash
		res.MetadataHealed = true
	}

	st.LastSyncedRevision = latest
	st.LastPullAt = p.now().Unix()
	if err := p.cache.applyRemoteOpts(toApply, remoteMeta, updateMetadata, st, stateWriteOptions{
		writerPath:     "pullApplyLocked",
		removedEntries: removed,
	}); err != nil {
		return nil, err
	}
	return res, nil
}

// PushResult 汇总一次 push 的结果
type PushResult struct {
	Pushed         int
	Healed         int  // 假冲突自愈：收养的快照条目数
	MetadataHealed bool // 假冲突自愈：metadata 快照哈希收养
	MetadataPushed bool
	LatestRevision int64
}

type pushPlan struct {
	st           *syncState
	dirty        []Entry
	pushedHashes map[string]string
	localMeta    []byte
	metaHash     string
}

func (p *ServerProvider) preparePushPlan() (*pushPlan, error) {
	var plan *pushPlan
	err := p.withVaultMutation(func() error {
		st, err := p.cache.loadState()
		if err != nil {
			return err
		}
		current, err := p.cache.collect()
		if err != nil {
			return err
		}
		dirty := p.collectDirty(st, current)
		pushedHashes := make(map[string]string, len(dirty))
		for _, e := range dirty {
			id := entryID(e.Kind, e.Grp, e.Key)
			if e.Deleted {
				pushedHashes[id] = ""
			} else {
				pushedHashes[id] = hashBytes(e.Ciphertext)
			}
		}
		localMeta, err := p.cache.readMetadata()
		if err != nil {
			return err
		}
		plan = &pushPlan{
			st:           st,
			dirty:        dirty,
			pushedHashes: pushedHashes,
			localMeta:    localMeta,
			metaHash:     hashBytes(localMeta),
		}
		return nil
	})
	return plan, err
}

func (p *ServerProvider) commitPushBatch(pushed []Entry, healed map[string]syncEntryState, latest int64, pushedHashes map[string]string, setRev bool) error {
	return p.withVaultMutation(func() error {
		st, err := p.cache.loadState()
		if err != nil {
			return err
		}
		current, err := p.cache.collect()
		if err != nil {
			return err
		}
		removed := make(map[string]bool)
		for id, hs := range healed {
			cur, exists := current[id]
			if exists && hashBytes(cur.Ciphertext) == hs.Hash {
				st.Entries[id] = hs
			}
		}
		for _, e := range pushed {
			id := entryID(e.Kind, e.Grp, e.Key)
			cur, exists := current[id]
			if e.Deleted {
				if !exists {
					delete(st.Entries, id)
					removed[id] = true
				}
				continue
			}
			if exists && hashBytes(cur.Ciphertext) == pushedHashes[id] {
				st.Entries[id] = syncEntryState{Revision: e.Revision, Hash: hashBytes(e.Ciphertext)}
			}
		}
		if setRev && latest > st.LastSyncedRevision {
			st.LastSyncedRevision = latest
		}
		return p.cache.saveStateOpts(st, stateWriteOptions{
			writerPath:     "pushLocked",
			removedEntries: removed,
		})
	})
}

// push 收集 dirty 条目并乐观锁批量推送（三段式：短锁读 → 无锁网络 → 短锁落盘）；409 时解析为 SyncConflictError
func (p *ServerProvider) push(ctx context.Context, remoteCandidates ...Entry) (*PushResult, error) {
	plan, err := p.preparePushPlan()
	if err != nil {
		return nil, err
	}
	st := plan.st
	dirty := plan.dirty
	localMeta := plan.localMeta

	// metadata：本地已改时，仅在远端未改（哈希仍等于快照）的情况下安全上传
	var metadataDirty, metadataConflict bool
	var metadataHealed bool
	var remoteMetaBlob []byte
	if plan.metaHash != st.MetadataHash {
		remoteMeta, err := p.api.GetMetadata(ctx, p.vault)
		if err != nil && !errors.Is(err, ErrVaultNotFound) {
			return nil, err
		}
		if errors.Is(err, ErrVaultNotFound) {
			// 远端无 metadata：本地版本可以安全上传
			metadataDirty = true
		} else if hashBytes(remoteMeta) == plan.metaHash {
			// 假冲突自愈：两端 metadata 字节一致，仅本地快照哈希失真，收养哈希。
			metadataHealed = true
			if err := p.withVaultMutation(func() error {
				curSt, err := p.cache.loadState()
				if err != nil {
					return err
				}
				curMeta, err := p.cache.readMetadata()
				if err != nil {
					return err
				}
				if hashBytes(curMeta) == plan.metaHash {
					curSt.MetadataHash = plan.metaHash
					return p.cache.saveStateOpts(curSt, stateWriteOptions{writerPath: "pushLocked"})
				}
				return nil
			}); err != nil {
				return nil, err
			}
		} else if hashBytes(remoteMeta) != st.MetadataHash {
			metadataConflict = true
			remoteMetaBlob = remoteMeta
		}
	}

	res := &PushResult{}
	if metadataHealed {
		res.MetadataHealed = true
	}
	// 分批推送：服务端单批上限 1000，条目总数过千时一次性推送会被整批拒绝。
	// 先按标识排序保证批次可复现；每批成功后立即持久化同步状态，中途失败不丢已推进度。
	sort.Slice(dirty, func(i, j int) bool {
		ki, kj := entryID(dirty[i].Kind, dirty[i].Grp, dirty[i].Key), entryID(dirty[j].Kind, dirty[j].Grp, dirty[j].Key)
		return ki < kj
	})
	batch := pushBatchSize()
	for len(dirty) > 0 {
		n := batch
		if n > len(dirty) {
			n = len(dirty)
		}
		chunk, rest := dirty[:n], dirty[n:]
		healedBatch := make(map[string]syncEntryState)
		for attempt := 0; ; attempt++ {
			pushed, latest, err := p.api.Push(ctx, p.vault, chunk)
			if err == nil {
				res.Pushed += len(pushed)
				res.LatestRevision = latest
				if err := p.commitPushBatch(pushed, healedBatch, latest, plan.pushedHashes, true); err != nil {
					return nil, err
				}
				break
			}
			var conflictErr *ConflictError
			if !errors.As(err, &conflictErr) {
				return nil, err
			}
			// 409 先做假冲突自愈：快照缺失导致的新增误判 + 两端密文一致 → 收养远端 revision。
			healed, retry, remaining, healMap, healErr := p.healFalseConflicts(ctx, chunk, conflictErr.Conflicts)
			if healErr != nil {
				// 自愈所需的查询失败：不落盘、不误报内容冲突，返回原始网络错误。
				return nil, healErr
			}
			for id, hs := range healMap {
				healedBatch[id] = hs
			}
			res.Healed += healed
			if len(remaining) > 0 {
				if len(healedBatch) > 0 {
					if saveErr := p.commitPushBatch(nil, healedBatch, 0, plan.pushedHashes, false); saveErr != nil {
						return nil, saveErr
					}
				}
				return nil, p.buildConflictError(ctx, remaining, metadataConflict, append(retry, rest...), remoteCandidates, localMeta, remoteMetaBlob)
			}
			chunk = retry
			if len(chunk) == 0 {
				if len(healedBatch) > 0 {
					if saveErr := p.commitPushBatch(nil, healedBatch, 0, plan.pushedHashes, false); saveErr != nil {
						return nil, saveErr
					}
				}
				break // 本批全部为假冲突并已收养，无需再次推送
			}
		}
		dirty = rest
	}

	if metadataConflict {
		return nil, &SyncConflictError{
			MetadataConflict: true,
			Metadata:         &MetadataConflictDetail{Local: localMeta, Remote: remoteMetaBlob},
		}
	}
	if metadataDirty {
		// 带快照哈希做乐观锁：期间其它设备改过 metadata → 409，不静默覆盖。
		// 首次上传（快照为空）没有 base，服务端按既有语义接受。
		if err := p.api.PutMetadataIfMatch(ctx, p.vault, localMeta, st.MetadataHash); err != nil {
			return nil, err
		}
		if err := p.withVaultMutation(func() error {
			curSt, err := p.cache.loadState()
			if err != nil {
				return err
			}
			curMeta, err := p.cache.readMetadata()
			if err != nil {
				return err
			}
			if hashBytes(curMeta) == plan.metaHash {
				curSt.MetadataHash = plan.metaHash
				return p.cache.saveStateOpts(curSt, stateWriteOptions{writerPath: "pushLocked"})
			}
			return nil
		}); err != nil {
			return nil, err
		}
		res.MetadataPushed = true
	}

	return res, nil
}

// healFalseConflicts 处理 409 中的"假冲突"：本地快照缺失导致条目以 BaseRevision=0
// 被当作新增推送，但远端密文与本地字节一致。此时优先用 409 返回的 ciphertext_sha256
// 零请求自愈；缺失时再用 Lookup 按需查询（老 server 自动回退 Pull(0)）。
func (p *ServerProvider) healFalseConflicts(ctx context.Context, dirty []Entry, conflicts []Conflict) (int, []Entry, []Conflict, map[string]syncEntryState, error) {
	dirtyByID := entryMap(dirty)
	healedIDs := make(map[string]syncEntryState)
	var needLookup []EntryKey
	for _, c := range conflicts {
		id := entryID(c.Kind, c.Grp, c.Key)
		e, ok := dirtyByID[id]
		if !ok || e.BaseRevision != 0 || e.Deleted {
			continue
		}
		if c.CiphertextSHA256 != "" {
			if !c.Deleted && hashBytes(e.Ciphertext) == c.CiphertextSHA256 {
				healedIDs[id] = syncEntryState{Revision: c.CurrentRevision, Hash: c.CiphertextSHA256}
			}
			continue
		}
		needLookup = append(needLookup, EntryKey{Kind: c.Kind, Grp: c.Grp, Key: c.Key})
	}

	if len(needLookup) > 0 {
		remote, _, err := p.lookupEntriesOrPull(ctx, needLookup, false)
		if err != nil {
			return 0, nil, nil, nil, err
		}
		remoteByID := entryMap(remote)
		for _, k := range needLookup {
			id := entryID(k.Kind, k.Grp, k.Key)
			remoteEntry, ok := remoteByID[id]
			if !ok || remoteEntry.Deleted {
				continue
			}
			e := dirtyByID[id]
			remoteHash := remoteEntry.CiphertextSHA256
			if remoteHash == "" {
				remoteHash = hashBytes(remoteEntry.Ciphertext)
			}
			localHash := hashBytes(e.Ciphertext)
			if localHash == remoteHash {
				healedIDs[id] = syncEntryState{Revision: remoteEntry.Revision, Hash: localHash}
			}
		}
	}

	retry := make([]Entry, 0, len(dirty))
	for _, e := range dirty {
		if _, ok := healedIDs[entryID(e.Kind, e.Grp, e.Key)]; !ok {
			retry = append(retry, e)
		}
	}
	var remaining []Conflict
	for _, c := range conflicts {
		if _, ok := healedIDs[entryID(c.Kind, c.Grp, c.Key)]; !ok {
			remaining = append(remaining, c)
		}
	}
	return len(healedIDs), retry, remaining, healedIDs, nil
}

// --- Provider 接口实现（message 参数对 server 无意义，忽略） ---

// Pull 增量拉取并落盘
func (p *ServerProvider) Pull() error {
	release, err := p.lockBlocking()
	if err != nil {
		return err
	}
	defer release()
	_, err = p.pull(context.Background())
	return err
}

// Push 推送本地待推送更改
func (p *ServerProvider) Push(_ string) error {
	release, err := p.lockBlocking()
	if err != nil {
		return err
	}
	defer release()
	_, err = p.push(context.Background())
	return err
}

// Sync 双向同步：先 pull 再 push
func (p *ServerProvider) Sync(_ string) error {
	release, err := p.lockBlocking()
	if err != nil {
		return err
	}
	defer release()
	pr, err := p.pull(context.Background())
	if err != nil {
		return err
	}
	_, err = p.push(context.Background(), pr.RemoteCandidates...)
	return err
}

// SyncResult 一次完整双向同步的汇总（cmd 层报告用）
type SyncResult struct {
	Pull  *PullResult
	Push  *PushResult
	Dirty int // 同步前本地待推送条目数
	// Healed 汇总本次同步自动修复的同步状态数量（快照收养 + metadata 哈希收养）。
	Healed int
}

func (r *SyncResult) computeHealed() int {
	if r == nil {
		return 0
	}
	healed := r.Push.Healed
	if r.Push.MetadataHealed {
		healed++
	}
	if r.Pull.MetadataHealed {
		healed++
	}
	return healed
}

// SyncWithReport 执行同步并返回详细结果
func (p *ServerProvider) SyncWithReport(ctx context.Context) (*SyncResult, error) {
	release, err := p.lockBlocking()
	if err != nil {
		return nil, err
	}
	defer release()

	st, err := p.cache.loadState()
	if err != nil {
		return nil, err
	}
	current, err := p.cache.collect()
	if err != nil {
		return nil, err
	}
	dirtyCount := len(p.collectDirty(st, current))

	pr, err := p.pull(ctx)
	if err != nil {
		return nil, err
	}
	sr, err := p.push(ctx, pr.RemoteCandidates...)
	if err != nil {
		return nil, err
	}
	result := &SyncResult{Pull: pr, Push: sr, Dirty: dirtyCount}
	result.Healed = result.computeHealed()
	return result, nil
}

// AcceptRemote 放弃本地改动，以远端为准：全量拉取覆盖本地（本地新增文件保留），
// 之后推送剩余本地新增条目
func (p *ServerProvider) AcceptRemote(ctx context.Context) error {
	release, err := p.lockBlocking()
	if err != nil {
		return err
	}
	defer release()
	return p.acceptRemoteLocked(ctx)
}

func (p *ServerProvider) acceptRemoteLocked(ctx context.Context) error {
	entries, latest, err := p.pullAllEntries(ctx, false)
	if err != nil {
		return err
	}
	if err := validateRemoteEntries(entries); err != nil {
		return err
	}
	blob, metaErr := p.api.GetMetadata(ctx, p.vault)
	if metaErr != nil && !errors.Is(metaErr, ErrVaultNotFound) {
		return metaErr
	}
	if err := p.withVaultMutation(func() error {
		st := newSyncState()
		for _, e := range entries {
			if e.Deleted {
				continue
			}
			st.Entries[entryID(e.Kind, e.Grp, e.Key)] = syncEntryState{Revision: e.Revision, Hash: hashBytes(e.Ciphertext)}
		}
		st.LastSyncedRevision = latest
		if metaErr == nil {
			st.MetadataHash = hashBytes(blob)
		}
		return p.cache.applyRemoteOpts(entries, blob, metaErr == nil, st, stateWriteOptions{
			writerPath:         "acceptRemoteLocked",
			allowEntryShrink:   true,
			allowEmptyMetadata: errors.Is(metaErr, ErrVaultNotFound),
		})
	}); err != nil {
		return err
	}
	_, err = p.push(ctx)
	return err
}

// ForcePush 放弃远端改动，以本地为准：先取远端当前 revision 作为 base，强制覆盖推送
func (p *ServerProvider) ForcePush(ctx context.Context) error {
	release, err := p.lockBlocking()
	if err != nil {
		return err
	}
	defer release()

	plan, err := p.preparePushPlan()
	if err != nil {
		return err
	}
	dirty := plan.dirty
	localMeta := plan.localMeta

	var latest int64
	if len(dirty) > 0 {
		keys := make([]EntryKey, 0, len(dirty))
		for _, e := range dirty {
			keys = append(keys, EntryKey{Kind: e.Kind, Grp: e.Grp, Key: e.Key})
		}
		remote, rev, err := p.lookupEntriesOrPull(ctx, keys, true)
		if err != nil {
			return err
		}
		if err := validateRemoteEntries(remote); err != nil {
			return err
		}
		latest = rev
		remoteRev := make(map[string]int64, len(remote))
		for _, e := range remote {
			remoteRev[entryID(e.Kind, e.Grp, e.Key)] = e.Revision
		}
		for i := range dirty {
			id := entryID(dirty[i].Kind, dirty[i].Grp, dirty[i].Key)
			dirty[i].BaseRevision = remoteRev[id] // 远端没有则为 0
		}
	} else {
		_, rev, err := p.api.Pull(ctx, p.vault, plan.st.LastSyncedRevision)
		if err != nil {
			return err
		}
		latest = rev
	}

	var pushed []Entry
	forceRemoved := make(map[string]bool)
	if len(dirty) > 0 {
		sort.Slice(dirty, func(i, j int) bool {
			return entryID(dirty[i].Kind, dirty[i].Grp, dirty[i].Key) < entryID(dirty[j].Kind, dirty[j].Grp, dirty[j].Key)
		})
		batch := pushBatchSize()
		for start := 0; start < len(dirty); start += batch {
			end := start + batch
			if end > len(dirty) {
				end = len(dirty)
			}
			ack, newLatest, err := p.api.Push(ctx, p.vault, dirty[start:end])
			if err != nil {
				return err
			}
			pushed = append(pushed, ack...)
			latest = newLatest
		}
	}

	// metadata 无条件以本地为准
	if err := p.api.PutMetadata(ctx, p.vault, localMeta); err != nil {
		return err
	}

	return p.withVaultMutation(func() error {
		st, err := p.cache.loadState()
		if err != nil {
			return err
		}
		current, err := p.cache.collect()
		if err != nil {
			return err
		}
		for _, e := range pushed {
			id := entryID(e.Kind, e.Grp, e.Key)
			cur, exists := current[id]
			if e.Deleted {
				if !exists {
					delete(st.Entries, id)
					forceRemoved[id] = true
				}
			} else if exists && hashBytes(cur.Ciphertext) == plan.pushedHashes[id] {
				st.Entries[id] = syncEntryState{Revision: e.Revision, Hash: hashBytes(e.Ciphertext)}
			}
		}
		st.LastSyncedRevision = latest
		if curMeta, err := p.cache.readMetadata(); err == nil && hashBytes(curMeta) == plan.metaHash {
			st.MetadataHash = plan.metaHash
		}
		return p.cache.saveStateOpts(st, stateWriteOptions{
			writerPath:     "forcePushLocked",
			removedEntries: forceRemoved,
		})
	})
}

// Status 返回本地缓存相对远端的同步状态描述
func (p *ServerProvider) Status() (string, error) {
	release, err := p.lockBlocking()
	if err != nil {
		return "", err
	}
	defer release()

	st, err := p.cache.loadState()
	if err != nil {
		return "", err
	}
	current, err := p.cache.collect()
	if err != nil {
		return "", err
	}
	dirty := p.collectDirty(st, current)
	localMeta, err := p.cache.readMetadata()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "provider: server (vault: %s)\n", p.vault)
	fmt.Fprintf(&b, "last_synced_revision: %d\n", st.LastSyncedRevision)
	fmt.Fprintf(&b, "待推送条目: %d", len(dirty))
	if hashBytes(localMeta) != st.MetadataHash {
		b.WriteString("\nmetadata: 有未同步改动")
	}
	if len(dirty) > 0 {
		b.WriteString("\n")
		for _, e := range dirty {
			marker := ""
			if e.Deleted {
				marker = " (已删除)"
			}
			fmt.Fprintf(&b, "  - %s/%s/%s%s\n", e.Kind, displayGrp(e.Grp, e.Key), displayKey(e.Kind, e.Key), marker)
		}
	}
	return b.String(), nil
}
