package provider

import (
	"context"
	"errors"
	"time"
)

// AutoSyncSkip 描述一次自动同步为何没有执行网络动作（"" 表示执行了）
type AutoSyncSkip string

const (
	// AutoSyncRan 表示本次执行了同步
	AutoSyncRan AutoSyncSkip = ""
	// AutoSyncSkipThrottled 节流窗口内，跳过拉取
	AutoSyncSkipThrottled AutoSyncSkip = "throttled"
	// AutoSyncSkipLocked 锁被同机其他进程持有，跳过（对方正在同步）
	AutoSyncSkipLocked AutoSyncSkip = "locked"
	// AutoSyncSkipClean 无待推送更改，跳过推送
	AutoSyncSkipClean AutoSyncSkip = "clean"
)

// AutoSyncEnabled 返回该 provider 的自动同步开关；旧 settings 缺省时为 true。
func (p *ServerProvider) AutoSyncEnabled() bool { return p.autoSync }

// SyncThrottleWindow 返回有效节流窗口（非法配置已在构造链路回退默认值）。
func (p *ServerProvider) SyncThrottleWindow() time.Duration { return p.syncThrottle }

const (
	// autoSyncBudget 是自动 pull/push 的最大等待时间，避免读写命令被慢网络拖住。
	autoSyncBudget = 2 * time.Second
	// blockingPushBudget 是 passwd 等关键写的确认预算。
	blockingPushBudget = 10 * time.Second
)

// AutoPushOutcome 汇总一次自动推送；Dirty 在推送失败时同样有效（警告文案用）
type AutoPushOutcome struct {
	Skip   AutoSyncSkip
	Dirty  int // 推送前待推送条目数
	Pushed int
}

// AutoPull 读命令前的 best-effort 增量拉取：锁忙或节流窗口内跳过（零网络），
// 拉取有 2s 超时预算，任何错误由调用方决定是否静默。
// refresh=true 绕过节流窗口强制拉取。
func (p *ServerProvider) AutoPull(ctx context.Context, throttle time.Duration, refresh bool) (*PullResult, AutoSyncSkip, error) {
	lock, err := acquireSyncLock(p.cache.dataPath, false)
	if err != nil {
		if errors.Is(err, errSyncLocked) {
			return nil, AutoSyncSkipLocked, nil
		}
		return nil, AutoSyncRan, err
	}
	defer lock.release()

	if !refresh && throttle > 0 {
		st, err := p.cache.loadState()
		if err != nil {
			return nil, AutoSyncRan, err
		}
		if p.now().Sub(time.Unix(st.LastPullAt, 0)) < throttle {
			return nil, AutoSyncSkipThrottled, nil
		}
	}
	pullCtx, cancel := context.WithTimeout(ctx, autoSyncBudget)
	defer cancel()
	res, err := p.pull(pullCtx)
	if err != nil {
		return nil, AutoSyncRan, err
	}
	return res, AutoSyncRan, nil
}

// AutoPush 写命令退出前的 best-effort 推送：无 dirty 零网络，锁忙跳过（对方正在同步，
// dirty 留给后续命令重试）。409 冲突以 *SyncConflictError 原样返回，本地 dirty 保留。
func (p *ServerProvider) AutoPush(ctx context.Context, budget time.Duration) (*AutoPushOutcome, error) {
	lock, err := acquireSyncLock(p.cache.dataPath, false)
	if err != nil {
		if errors.Is(err, errSyncLocked) {
			return &AutoPushOutcome{Skip: AutoSyncSkipLocked}, nil
		}
		return nil, err
	}
	defer lock.release()

	st, err := p.cache.loadState()
	if err != nil {
		return nil, err
	}
	current, err := p.cache.collect()
	if err != nil {
		return nil, err
	}
	dirty := len(p.collectDirty(st, current))
	if dirty == 0 {
		return &AutoPushOutcome{Skip: AutoSyncSkipClean}, nil
	}

	out := &AutoPushOutcome{Dirty: dirty}
	if budget <= 0 {
		budget = autoSyncBudget
	}
	pushCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	res, err := p.push(pushCtx)
	if res != nil {
		out.Pushed = res.Pushed
	}
	return out, err
}

// PushBlocking 关键写（改口令等）的阻塞推送：结果必须在返回前确认，
// 预算由调用方 ctx 控制（建议 10s 量级）。
func (p *ServerProvider) PushBlocking(ctx context.Context) (*PushResult, error) {
	release, err := p.lockBlocking()
	if err != nil {
		return nil, err
	}
	defer release()
	pushCtx, cancel := context.WithTimeout(ctx, blockingPushBudget)
	defer cancel()
	return p.push(pushCtx)
}
