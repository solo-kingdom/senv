package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wii/senv/internal/storage"
)

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

// NewServerProvider 以 HTTP client 构造 server provider（CLI 使用）
func NewServerProvider(address, token, configPath, dataPath, vault string) *ServerProvider {
	return newServerProvider(newServerClient(address, token), configPath, dataPath, vault)
}

// newServerProviderWithBinding 是测试专用构造：注入合成 vault 绑定验证归属校验。
func newServerProviderWithBinding(api serverAPI, configPath, dataPath, vault string, binding vaultBinding) *ServerProvider {
	p := newServerProvider(api, configPath, dataPath, vault)
	b := binding
	p.cache.binding = &b
	return p
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
	needRefresh := false
	for _, c := range conflicts {
		candidate, ok := known[entryID(c.Kind, c.Grp, c.Key)]
		if !ok || candidate.Revision != c.CurrentRevision {
			needRefresh = true
			break
		}
	}
	if needRefresh {
		if entries, _, err := p.api.Pull(ctx, p.vault, 0); err == nil {
			if err := validateRemoteEntries(entries); err == nil {
				known = entryMap(entries)
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
	return p.withVaultMutation(func() error { return p.bootstrapLocked(ctx) })
}

func (p *ServerProvider) bootstrapLocked(ctx context.Context) error {
	blob, err := p.api.GetMetadata(ctx, p.vault)
	if err != nil {
		return err
	}
	entries, latest, err := p.api.Pull(ctx, p.vault, 0)
	if err != nil {
		return err
	}
	if err := validateRemoteEntries(entries); err != nil {
		return err
	}
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

// pull 增量拉取并落盘；本地 dirty 的条目不被远端覆盖（留给 push 乐观锁判定）
func (p *ServerProvider) pull(ctx context.Context) (*PullResult, error) {
	var result *PullResult
	err := p.withVaultMutation(func() error {
		var err error
		result, err = p.pullLocked(ctx)
		return err
	})
	return result, err
}

func (p *ServerProvider) pullLocked(ctx context.Context) (*PullResult, error) {
	st, err := p.cache.loadState()
	if err != nil {
		return nil, err
	}

	entries, latest, err := p.api.Pull(ctx, p.vault, st.LastSyncedRevision)
	if errors.Is(err, ErrVaultNotFound) {
		entries, latest = nil, 0
	} else if err != nil {
		return nil, err
	}
	// Validate every returned identity, including dirty entries that will be
	// skipped, before any mutable filesystem operation.
	if err := validateRemoteEntries(entries); err != nil {
		return nil, err
	}
	current, err := p.cache.collect()
	if err != nil {
		return nil, err
	}
	dirty := dirtyIDs(p.collectDirty(st, current))
	res := &PullResult{LatestRevision: latest}
	toApply := make([]Entry, 0, len(entries))
	// 本次合法消失的条目（远端 tombstone），供状态防退化护栏放行。
	removed := make(map[string]bool)
	for _, e := range entries {
		id := entryID(e.Kind, e.Grp, e.Key)
		if dirty[id] {
			res.SkippedDirty++
			res.RemoteCandidates = append(res.RemoteCandidates, e)
			continue
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
	remoteMeta, err := p.api.GetMetadata(ctx, p.vault)
	if err != nil && !errors.Is(err, ErrVaultNotFound) {
		return nil, err
	}
	localHash := hashBytes(localMeta)
	remoteHash := hashBytes(remoteMeta)
	updateMetadata := false
	if remoteHash != localHash {
		if localHash == st.MetadataHash {
			st.MetadataHash = remoteHash
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
		writerPath:     "pullLocked",
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

// push 收集 dirty 条目并乐观锁批量推送；409 时解析为 SyncConflictError
func (p *ServerProvider) push(ctx context.Context, remoteCandidates ...Entry) (*PushResult, error) {
	var result *PushResult
	err := p.withVaultMutation(func() error {
		var err error
		result, err = p.pushLocked(ctx, remoteCandidates)
		return err
	})
	return result, err
}

func (p *ServerProvider) pushLocked(ctx context.Context, remoteCandidates []Entry) (*PushResult, error) {
	st, err := p.cache.loadState()
	if err != nil {
		return nil, err
	}
	current, err := p.cache.collect()
	if err != nil {
		return nil, err
	}
	dirty := p.collectDirty(st, current)

	// metadata：本地已改时，仅在远端未改（哈希仍等于快照）的情况下安全上传
	var metadataDirty, metadataConflict bool
	var metadataHealed bool
	var remoteMetaBlob []byte
	localMeta, err := p.cache.readMetadata()
	if err != nil {
		return nil, err
	}
	if hashBytes(localMeta) != st.MetadataHash {
		remoteMeta, err := p.api.GetMetadata(ctx, p.vault)
		if err != nil && !errors.Is(err, ErrVaultNotFound) {
			return nil, err
		}
		if errors.Is(err, ErrVaultNotFound) {
			// 远端无 metadata：本地版本可以安全上传
			metadataDirty = true
		} else if hashBytes(remoteMeta) == hashBytes(localMeta) {
			// 假冲突自愈：两端 metadata 字节一致，仅本地快照哈希失真，收养哈希。
			st.MetadataHash = hashBytes(localMeta)
			metadataHealed = true
		} else if hashBytes(remoteMeta) != st.MetadataHash {
			metadataConflict = true
			remoteMetaBlob = remoteMeta
		}
	}

	res := &PushResult{}
	if metadataHealed {
		res.MetadataHealed = true
	}
	// removed 收集本次推送的删除标记（合法消失的快照），供防退化护栏放行。
	removed := make(map[string]bool)
	if len(dirty) > 0 {
		for attempt := 0; ; attempt++ {
			pushed, latest, err := p.api.Push(ctx, p.vault, dirty)
			if err == nil {
				res.Pushed = len(pushed)
				res.LatestRevision = latest
				for _, e := range pushed {
					id := entryID(e.Kind, e.Grp, e.Key)
					if e.Deleted {
						delete(st.Entries, id)
						removed[id] = true
					} else {
						st.Entries[id] = syncEntryState{Revision: e.Revision, Hash: hashBytes(e.Ciphertext)}
					}
				}
				st.LastSyncedRevision = latest
				break
			}
			var conflictErr *ConflictError
			if !errors.As(err, &conflictErr) {
				return nil, err
			}
			// 409 先做假冲突自愈：快照缺失导致的新增误判 + 两端密文一致 → 收养远端 revision。
			healed, retry, remaining, healErr := p.healFalseConflicts(ctx, st, dirty, conflictErr.Conflicts)
			if healErr != nil {
				// 自愈所需的 Pull(0) 失败：不落盘、不误报内容冲突，返回原始网络错误。
				return nil, healErr
			}
			res.Healed += healed
			if len(remaining) > 0 {
				return nil, p.buildConflictError(ctx, remaining, metadataConflict, retry, remoteCandidates, localMeta, remoteMetaBlob)
			}
			dirty = retry
			if len(dirty) == 0 {
				break // 全部为假冲突并已收养，无需再次推送
			}
		}
	}

	if metadataConflict {
		return nil, &SyncConflictError{
			MetadataConflict: true,
			Metadata:         &MetadataConflictDetail{Local: localMeta, Remote: remoteMetaBlob},
		}
	}
	if metadataDirty {
		if err := p.api.PutMetadata(ctx, p.vault, localMeta); err != nil {
			return nil, err
		}
		st.MetadataHash = hashBytes(localMeta)
		res.MetadataPushed = true
	}

	if err := p.cache.saveStateOpts(st, stateWriteOptions{
		writerPath:     "pushLocked",
		removedEntries: removed,
	}); err != nil {
		return nil, err
	}
	return res, nil
}

// healFalseConflicts 处理 409 中的"假冲突"：本地快照缺失导致条目以 BaseRevision=0
// 被当作新增推送，但远端密文与本地字节一致。此时收养远端 revision 修复快照，
// 而不是报内容冲突；其余冲突原样返回，由调用方走既有冲突路径。
// 返回：收养条数、剔除已收养条目后的待推送清单、无法自愈的冲突清单。
func (p *ServerProvider) healFalseConflicts(ctx context.Context, st *syncState, dirty []Entry, conflicts []Conflict) (int, []Entry, []Conflict, error) {
	// 只尝试"本地视为新增且非删除标记"的冲突；真实内容差异与删除冲突不自愈。
	candidates := make(map[string]bool, len(conflicts))
	for _, c := range conflicts {
		id := entryID(c.Kind, c.Grp, c.Key)
		for _, e := range dirty {
			if entryID(e.Kind, e.Grp, e.Key) == id && e.BaseRevision == 0 && !e.Deleted {
				candidates[id] = true
			}
		}
	}
	if len(candidates) == 0 {
		return 0, dirty, conflicts, nil
	}

	remote, _, err := p.api.Pull(ctx, p.vault, 0)
	if err != nil {
		return 0, nil, nil, err
	}
	remoteByID := make(map[string]Entry, len(remote))
	for _, e := range remote {
		remoteByID[entryID(e.Kind, e.Grp, e.Key)] = e
	}

	healedIDs := make(map[string]bool, len(candidates))
	for id := range candidates {
		remoteEntry, ok := remoteByID[id]
		if !ok || remoteEntry.Deleted {
			continue
		}
		for _, e := range dirty {
			if entryID(e.Kind, e.Grp, e.Key) != id || e.Deleted {
				continue
			}
			if hashBytes(e.Ciphertext) == hashBytes(remoteEntry.Ciphertext) {
				st.Entries[id] = syncEntryState{Revision: remoteEntry.Revision, Hash: hashBytes(remoteEntry.Ciphertext)}
				healedIDs[id] = true
			}
			break
		}
	}

	retry := make([]Entry, 0, len(dirty))
	for _, e := range dirty {
		if !healedIDs[entryID(e.Kind, e.Grp, e.Key)] {
			retry = append(retry, e)
		}
	}
	var remaining []Conflict
	for _, c := range conflicts {
		if !healedIDs[entryID(c.Kind, c.Grp, c.Key)] {
			remaining = append(remaining, c)
		}
	}
	return len(healedIDs), retry, remaining, nil
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
	return p.withVaultMutation(func() error { return p.acceptRemoteLocked(ctx) })
}

func (p *ServerProvider) acceptRemoteLocked(ctx context.Context) error {
	entries, latest, err := p.api.Pull(ctx, p.vault, 0)
	if err != nil {
		return err
	}
	if err := validateRemoteEntries(entries); err != nil {
		return err
	}
	blob, err := p.api.GetMetadata(ctx, p.vault)
	if err != nil && !errors.Is(err, ErrVaultNotFound) {
		return err
	}
	st := newSyncState()
	for _, e := range entries {
		if e.Deleted {
			continue
		}
		st.Entries[entryID(e.Kind, e.Grp, e.Key)] = syncEntryState{Revision: e.Revision, Hash: hashBytes(e.Ciphertext)}
	}
	st.LastSyncedRevision = latest
	if err == nil {
		st.MetadataHash = hashBytes(blob)
	}
	if err := p.cache.applyRemoteOpts(entries, blob, err == nil, st, stateWriteOptions{
		writerPath:         "acceptRemoteLocked",
		allowEntryShrink:   true,
		allowEmptyMetadata: errors.Is(err, ErrVaultNotFound),
	}); err != nil {
		return err
	}
	_, err = p.pushLocked(ctx, nil)
	return err
}

// ForcePush 放弃远端改动，以本地为准：先取远端当前 revision 作为 base，强制覆盖推送
func (p *ServerProvider) ForcePush(ctx context.Context) error {
	release, err := p.lockBlocking()
	if err != nil {
		return err
	}
	defer release()
	return p.withVaultMutation(func() error { return p.forcePushLocked(ctx) })
}

func (p *ServerProvider) forcePushLocked(ctx context.Context) error {
	remote, latest, err := p.api.Pull(ctx, p.vault, 0)
	if err != nil {
		return err
	}
	if err := validateRemoteEntries(remote); err != nil {
		return err
	}
	remoteRev := make(map[string]int64, len(remote))
	for _, e := range remote {
		remoteRev[entryID(e.Kind, e.Grp, e.Key)] = e.Revision
	}

	st, err := p.cache.loadState()
	if err != nil {
		return err
	}
	current, err := p.cache.collect()
	if err != nil {
		return err
	}
	dirty := p.collectDirty(st, current)
	// forceRemoved 收集被推送的删除标记，供防退化护栏放行。
	forceRemoved := make(map[string]bool)
	// 以远端当前 revision 为 base：server 端对应条目被本地版本覆盖
	for i := range dirty {
		id := entryID(dirty[i].Kind, dirty[i].Grp, dirty[i].Key)
		dirty[i].BaseRevision = remoteRev[id] // 远端没有则为 0
	}

	if len(dirty) > 0 {
		pushed, newLatest, err := p.api.Push(ctx, p.vault, dirty)
		if err != nil {
			return err
		}
		for _, e := range pushed {
			id := entryID(e.Kind, e.Grp, e.Key)
			if e.Deleted {
				delete(st.Entries, id)
				forceRemoved[id] = true
			} else {
				st.Entries[id] = syncEntryState{Revision: e.Revision, Hash: hashBytes(e.Ciphertext)}
			}
		}
		st.LastSyncedRevision = newLatest
	} else {
		st.LastSyncedRevision = latest
	}

	// metadata 无条件以本地为准
	localMeta, err := p.cache.readMetadata()
	if err != nil {
		return err
	}
	if err := p.api.PutMetadata(ctx, p.vault, localMeta); err != nil {
		return err
	}
	st.MetadataHash = hashBytes(localMeta)
	return p.cache.saveStateOpts(st, stateWriteOptions{
		writerPath:     "forcePushLocked",
		removedEntries: forceRemoved,
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
