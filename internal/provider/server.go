package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ServerProvider 是 server provider：本地缓存（复用 storage.Manager 文件格式）为工作副本，
// senv-server 为远端。同步 = 增量 pull 落盘 + 收集 dirty 条目乐观锁批量 push。
type ServerProvider struct {
	api   serverAPI
	cache *localCache
	vault string
}

// newServerProvider 构造 server provider（接口实现），api 可注入以便测试
func newServerProvider(api serverAPI, configPath, dataPath, vault string) *ServerProvider {
	return &ServerProvider{
		api:   api,
		cache: &localCache{configPath: configPath, dataPath: dataPath},
		vault: vault,
	}
}

// NewServerProvider 以 HTTP client 构造 server provider（CLI 使用）
func NewServerProvider(address, token, configPath, dataPath, vault string) *ServerProvider {
	return newServerProvider(newServerClient(address, token), configPath, dataPath, vault)
}

// SyncConflictError 同步因乐观锁冲突中止；两端数据均未改动。
// 附解决指引：accept-remote（放弃本地）或 force-push（放弃远端）。
type SyncConflictError struct {
	Conflicts        []Conflict
	MetadataConflict bool
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

// Bootstrap 初始化本地缓存：拉取 metadata 与全部条目落盘，建立同步状态。
// vault 在 server 端不存在时返回 ErrVaultNotFound。
func (p *ServerProvider) Bootstrap(ctx context.Context) error {
	blob, err := p.api.GetMetadata(ctx, p.vault)
	if err != nil {
		return err
	}
	entries, latest, err := p.api.Pull(ctx, p.vault, 0)
	if err != nil {
		return err
	}
	if err := p.cache.writeMetadata(blob); err != nil {
		return err
	}
	st := newSyncState()
	st.MetadataHash = hashBytes(blob)
	st.LastSyncedRevision = latest
	for _, e := range entries {
		if err := p.cache.apply(e); err != nil {
			return err
		}
		st.Entries[entryID(e.Kind, e.Grp, e.Key)] = syncEntryState{Revision: e.Revision, Hash: hashBytes(e.Ciphertext)}
	}
	return p.cache.saveState(st)
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
	Applied          int  // 落盘的条目数
	SkippedDirty     int  // 因本地有未推送改动而跳过的条目数
	MetadataUpdated  bool // metadata 是否被远端版本更新
	MetadataConflict bool // metadata 两端均已修改
	LatestRevision   int64
}

// pull 增量拉取并落盘；本地 dirty 的条目不被远端覆盖（留给 push 乐观锁判定）
func (p *ServerProvider) pull(ctx context.Context) (*PullResult, error) {
	st, err := p.cache.loadState()
	if err != nil {
		return nil, err
	}
	current, err := p.cache.collect()
	if err != nil {
		return nil, err
	}
	dirty := dirtyIDs(p.collectDirty(st, current))

	entries, latest, err := p.api.Pull(ctx, p.vault, st.LastSyncedRevision)
	if errors.Is(err, ErrVaultNotFound) {
		// 远端尚无此 vault（首次从本地推送的场景）：视为空远端
		entries, latest = nil, 0
	} else if err != nil {
		return nil, err
	}
	res := &PullResult{LatestRevision: latest}
	for _, e := range entries {
		id := entryID(e.Kind, e.Grp, e.Key)
		if dirty[id] {
			res.SkippedDirty++
			continue
		}
		if err := p.cache.apply(e); err != nil {
			return nil, err
		}
		res.Applied++
		if e.Deleted {
			delete(st.Entries, id)
		} else {
			st.Entries[id] = syncEntryState{Revision: e.Revision, Hash: hashBytes(e.Ciphertext)}
		}
	}

	// metadata：本地未改而远端已改 → 接受远端；两端均改 → 标记冲突留待 push 阶段
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
	if remoteHash != localHash {
		if localHash == st.MetadataHash {
			if err := p.cache.writeMetadata(remoteMeta); err != nil {
				return nil, err
			}
			st.MetadataHash = remoteHash
			res.MetadataUpdated = true
		} else {
			res.MetadataConflict = true
		}
	}

	st.LastSyncedRevision = latest
	if err := p.cache.saveState(st); err != nil {
		return nil, err
	}
	return res, nil
}

// PushResult 汇总一次 push 的结果
type PushResult struct {
	Pushed         int
	MetadataPushed bool
	LatestRevision int64
}

// push 收集 dirty 条目并乐观锁批量推送；409 时解析为 SyncConflictError
func (p *ServerProvider) push(ctx context.Context) (*PushResult, error) {
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
	localMeta, err := p.cache.readMetadata()
	if err != nil {
		return nil, err
	}
	if hashBytes(localMeta) != st.MetadataHash {
		metadataDirty = true
		remoteMeta, err := p.api.GetMetadata(ctx, p.vault)
		if err != nil && !errors.Is(err, ErrVaultNotFound) {
			return nil, err
		}
		if !errors.Is(err, ErrVaultNotFound) && hashBytes(remoteMeta) != st.MetadataHash {
			metadataConflict = true
		}
	}

	res := &PushResult{}
	if len(dirty) > 0 {
		pushed, latest, err := p.api.Push(ctx, p.vault, dirty)
		if err != nil {
			var conflictErr *ConflictError
			if errors.As(err, &conflictErr) {
				return nil, &SyncConflictError{Conflicts: conflictErr.Conflicts, MetadataConflict: metadataConflict}
			}
			return nil, err
		}
		res.Pushed = len(pushed)
		res.LatestRevision = latest
		for _, e := range pushed {
			id := entryID(e.Kind, e.Grp, e.Key)
			if e.Deleted {
				delete(st.Entries, id)
			} else {
				st.Entries[id] = syncEntryState{Revision: e.Revision, Hash: hashBytes(e.Ciphertext)}
			}
		}
		st.LastSyncedRevision = latest
	}

	if metadataConflict {
		return nil, &SyncConflictError{MetadataConflict: true}
	}
	if metadataDirty {
		if err := p.api.PutMetadata(ctx, p.vault, localMeta); err != nil {
			return nil, err
		}
		st.MetadataHash = hashBytes(localMeta)
		res.MetadataPushed = true
	}

	if err := p.cache.saveState(st); err != nil {
		return nil, err
	}
	return res, nil
}

// --- Provider 接口实现（message 参数对 server 无意义，忽略） ---

// Pull 增量拉取并落盘
func (p *ServerProvider) Pull() error {
	_, err := p.pull(context.Background())
	return err
}

// Push 推送本地待推送更改
func (p *ServerProvider) Push(_ string) error {
	_, err := p.push(context.Background())
	return err
}

// Sync 双向同步：先 pull 再 push
func (p *ServerProvider) Sync(_ string) error {
	if _, err := p.pull(context.Background()); err != nil {
		return err
	}
	_, err := p.push(context.Background())
	return err
}

// SyncResult 一次完整双向同步的汇总（cmd 层报告用）
type SyncResult struct {
	Pull  *PullResult
	Push  *PushResult
	Dirty int // 同步前本地待推送条目数
}

// SyncWithReport 执行同步并返回详细结果
func (p *ServerProvider) SyncWithReport(ctx context.Context) (*SyncResult, error) {
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
	sr, err := p.push(ctx)
	if err != nil {
		return nil, err
	}
	return &SyncResult{Pull: pr, Push: sr, Dirty: dirtyCount}, nil
}

// AcceptRemote 放弃本地改动，以远端为准：全量拉取覆盖本地（本地新增文件保留），
// 之后推送剩余本地新增条目
func (p *ServerProvider) AcceptRemote(ctx context.Context) error {
	entries, latest, err := p.api.Pull(ctx, p.vault, 0)
	if err != nil {
		return err
	}
	st := newSyncState()
	for _, e := range entries {
		if err := p.cache.apply(e); err != nil {
			return err
		}
		st.Entries[entryID(e.Kind, e.Grp, e.Key)] = syncEntryState{Revision: e.Revision, Hash: hashBytes(e.Ciphertext)}
	}
	st.LastSyncedRevision = latest
	// metadata 同样以远端为准
	blob, err := p.api.GetMetadata(ctx, p.vault)
	if err != nil && !errors.Is(err, ErrVaultNotFound) {
		return err
	}
	if err == nil {
		if err := p.cache.writeMetadata(blob); err != nil {
			return err
		}
		st.MetadataHash = hashBytes(blob)
	}
	if err := p.cache.saveState(st); err != nil {
		return err
	}
	// 本地新增文件（远端没有）作为新条目推送
	_, err = p.push(ctx)
	return err
}

// ForcePush 放弃远端改动，以本地为准：先取远端当前 revision 作为 base，强制覆盖推送
func (p *ServerProvider) ForcePush(ctx context.Context) error {
	remote, latest, err := p.api.Pull(ctx, p.vault, 0)
	if err != nil {
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
	return p.cache.saveState(st)
}

// Status 返回本地缓存相对远端的同步状态描述
func (p *ServerProvider) Status() (string, error) {
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
