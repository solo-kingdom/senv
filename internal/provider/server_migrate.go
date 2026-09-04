package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// ErrTargetNotEmpty 目标端已有与源不一致的数据，未显式确认覆盖时中止迁移
var ErrTargetNotEmpty = errors.New("目标非空：存在与源不一致的数据")

// MigrateResult 迁移结果：按 kind 计数 + 幂等跳过数
type MigrateResult struct {
	Counts          map[string]int // kind -> 实际搬运条数
	Skipped         int            // 两端已一致、幂等跳过的条数
	MetadataMoved   bool           // metadata blob 是否实际搬运
	MetadataSkipped bool           // metadata 两端一致而跳过
	ExtraKept       int            // 目标端额外条目（force 时保留）
	Total           int            // 源端条目总数
}

// maxMigrateBatch 与 server 单批上限一致
const maxMigrateBatch = 1000

// entryConflictError 目标端存在与源不一致的条目（列出清单）
type entryConflictError struct {
	lines []string
	extra []string
}

func (e *entryConflictError) Error() string {
	msg := "目标端已有与源不一致的数据，迁移中止（目标未改动）:\n"
	for _, l := range e.lines {
		msg += "  冲突: " + l + "\n"
	}
	for _, l := range e.extra {
		msg += "  目标额外: " + l + "\n"
	}
	msg += "确认覆盖请加 --force（以源为准覆盖目标）"
	return msg
}

// MigrateToServer 把本地缓存（git 模式数据仓）的全部密文条目搬到 server vault。
// 幂等：两端一致的条目跳过；中断后重跑继续直至完成。不触碰明文、不需要 vault 口令。
func (p *ServerProvider) MigrateToServer(ctx context.Context, force bool) (*MigrateResult, error) {
	release, err := p.lockBlocking()
	if err != nil {
		return nil, err
	}
	defer release()
	var result *MigrateResult
	err = p.withVaultMutation(func() error {
		var innerErr error
		result, innerErr = p.migrateToServerLocked(ctx, force)
		return innerErr
	})
	return result, err
}

func (p *ServerProvider) migrateToServerLocked(ctx context.Context, force bool) (*MigrateResult, error) {
	local, err := p.cache.collect()
	if err != nil {
		return nil, err
	}
	localMeta, err := p.cache.readMetadata()
	if err != nil {
		return nil, err
	}
	if len(localMeta) == 0 {
		return nil, fmt.Errorf("本地 metadata.json 不存在，请先初始化 vault")
	}

	remote, _, err := p.api.Pull(ctx, p.vault, 0)
	if err != nil && !errors.Is(err, ErrVaultNotFound) {
		return nil, err
	}
	if err := validateRemoteEntries(remote); err != nil {
		return nil, err
	}
	remoteByID := make(map[string]Entry, len(remote))
	for _, e := range remote {
		remoteByID[entryID(e.Kind, e.Grp, e.Key)] = e
	}

	// 分类：新增 / 已一致（跳过）/ 冲突（目标内容与源不同）；目标额外条目
	var toPush []Entry
	var conflicts, extra []string
	res := &MigrateResult{Counts: make(map[string]int), Total: len(local)}
	ids := make([]string, 0, len(local))
	for id := range local {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := local[id]
		r, ok := remoteByID[id]
		switch {
		case !ok || r.Deleted:
			e.BaseRevision = 0
			if ok && r.Deleted {
				e.BaseRevision = r.Revision
			}
			toPush = append(toPush, e)
		case hashBytes(r.Ciphertext) == hashBytes(e.Ciphertext):
			res.Skipped++ // 幂等：已完成条目
		case force:
			e.BaseRevision = r.Revision // 显式覆盖：以本地为准
			toPush = append(toPush, e)
		default:
			conflicts = append(conflicts, fmt.Sprintf("%s/%s/%s", e.Kind, displayGrp(e.Grp, e.Key), displayKey(e.Kind, e.Key)))
		}
	}
	for _, r := range remote {
		if r.Deleted {
			continue
		}
		if _, ok := local[entryID(r.Kind, r.Grp, r.Key)]; !ok {
			if force {
				res.ExtraKept++
			} else {
				extra = append(extra, fmt.Sprintf("%s/%s/%s", r.Kind, displayGrp(r.Grp, r.Key), displayKey(r.Kind, r.Key)))
			}
		}
	}
	if len(conflicts) > 0 || len(extra) > 0 {
		return nil, &entryConflictError{lines: conflicts, extra: extra}
	}

	// metadata：远端缺失或与本地一致以外的情形都需要搬运；force 时无条件以本地为准
	remoteMeta, metaErr := p.api.GetMetadata(ctx, p.vault)
	switch {
	case errors.Is(metaErr, ErrVaultNotFound):
		// 远端无 metadata：直接写入
	case metaErr != nil:
		return nil, metaErr
	case hashBytes(remoteMeta) == hashBytes(localMeta):
		res.MetadataSkipped = true
	case !force:
		return nil, &entryConflictError{lines: []string{"vault metadata（远端与本地不同）"}}
	}

	// 分批推送（server 单批上限 1000）
	for start := 0; start < len(toPush); start += maxMigrateBatch {
		end := start + maxMigrateBatch
		if end > len(toPush) {
			end = len(toPush)
		}
		if _, _, err := p.api.Push(ctx, p.vault, toPush[start:end]); err != nil {
			return res, fmt.Errorf("推送条目失败（已完成 %d 条，可安全重试）: %w", start, err)
		}
		for _, e := range toPush[start:end] {
			res.Counts[e.Kind]++
		}
	}
	if !res.MetadataSkipped {
		if err := p.api.PutMetadata(ctx, p.vault, localMeta); err != nil {
			return res, fmt.Errorf("写入 metadata 失败（条目已搬完，可安全重试）: %w", err)
		}
		res.MetadataMoved = true
	}
	return res, nil
}

// MigrateFromServer 把 server vault 的全部密文条目落回本地缓存（git 仓）。
// 幂等；冲突分析先于任何写入（未确认覆盖时目标不变）。
// 写完后同步状态与远端对齐（之后可直接以 server 模式同步）。
func (p *ServerProvider) MigrateFromServer(ctx context.Context, force bool) (*MigrateResult, error) {
	release, err := p.lockBlocking()
	if err != nil {
		return nil, err
	}
	defer release()
	var result *MigrateResult
	err = p.withVaultMutation(func() error {
		var innerErr error
		result, innerErr = p.migrateFromServerLocked(ctx, force)
		return innerErr
	})
	return result, err
}

func (p *ServerProvider) migrateFromServerLocked(ctx context.Context, force bool) (*MigrateResult, error) {
	remote, latest, err := p.api.Pull(ctx, p.vault, 0)
	if err != nil {
		return nil, err
	}
	if err := validateRemoteEntries(remote); err != nil {
		return nil, err
	}
	remoteMeta, err := p.api.GetMetadata(ctx, p.vault)
	if err != nil {
		return nil, err
	}

	local, err := p.cache.collect()
	if err != nil {
		return nil, err
	}
	localMeta, err := p.cache.readMetadata()
	if err != nil {
		return nil, err
	}

	res := &MigrateResult{Counts: make(map[string]int), Total: len(remote)}

	// 第一阶段：纯分析，不写目标
	var toWrite []Entry // 需要落盘的远端条目（含 force 覆盖与删除标记）
	var conflicts, extra []string
	remoteByID := make(map[string]Entry, len(remote))
	for _, e := range remote {
		remoteByID[entryID(e.Kind, e.Grp, e.Key)] = e
	}
	ids := make([]string, 0, len(remote))
	for id := range remoteByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := remoteByID[id]
		l, ok := local[id]
		switch {
		case e.Deleted:
			if ok {
				if force {
					toWrite = append(toWrite, e)
				} else {
					conflicts = append(conflicts, fmt.Sprintf("%s/%s/%s（远端已删除，本地存在）", e.Kind, displayGrp(e.Grp, e.Key), displayKey(e.Kind, e.Key)))
				}
			}
		case !ok:
			toWrite = append(toWrite, e)
		case hashBytes(l.Ciphertext) == hashBytes(e.Ciphertext):
			res.Skipped++
		case force:
			toWrite = append(toWrite, e)
		default:
			conflicts = append(conflicts, fmt.Sprintf("%s/%s/%s", e.Kind, displayGrp(e.Grp, e.Key), displayKey(e.Kind, e.Key)))
		}
	}
	// 本地额外条目（远端没有）：未 force 时中止
	for id := range local {
		if _, ok := remoteByID[id]; !ok {
			parts := splitID(id)
			if force {
				res.ExtraKept++
			} else {
				extra = append(extra, fmt.Sprintf("%s/%s/%s", parts[0], displayGrp(parts[1], parts[2]), displayKey(parts[0], parts[2])))
			}
		}
	}
	// metadata 检查
	writeMetadata := false
	switch {
	case len(localMeta) == 0:
		writeMetadata = true
	case hashBytes(localMeta) == hashBytes(remoteMeta):
		res.MetadataSkipped = true
	case force:
		writeMetadata = true
	default:
		conflicts = append(conflicts, "vault metadata（本地已存在且与远端不同）")
	}
	if len(conflicts) > 0 || len(extra) > 0 {
		return nil, &entryConflictError{lines: conflicts, extra: extra}
	}

	// 第二阶段：建立目标快照后统一应用，sync state 最后提交。
	st := newSyncState()
	st.LastSyncedRevision = latest
	for _, e := range toWrite {
		res.Counts[e.Kind]++
	}
	for id, e := range remoteByID {
		if e.Deleted {
			continue
		}
		st.Entries[id] = syncEntryState{Revision: e.Revision, Hash: hashBytes(e.Ciphertext)}
	}
	if writeMetadata {
		res.MetadataMoved = true
	}
	st.MetadataHash = hashBytes(remoteMeta)
	if err := p.cache.applyRemoteOpts(toWrite, remoteMeta, writeMetadata, st, stateWriteOptions{
		writerPath:       "migrateFromServerLocked",
		allowEntryShrink: true,
	}); err != nil {
		res.MetadataMoved = false
		return res, err
	}
	return res, nil
}

func splitID(id string) []string {
	parts := make([]string, 0, 3)
	start := 0
	for i := 0; i < len(id) && len(parts) < 2; i++ {
		if id[i] == 0 {
			parts = append(parts, id[start:i])
			start = i + 1
		}
	}
	return append(parts, id[start:])
}
