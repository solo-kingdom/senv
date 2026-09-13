package tui

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/text"
)

// envSnap is one in-memory vault view shared by env Tab, search, deref and
// AI credential-ref collection. Values are plaintext of env vars only.
type envSnap struct {
	Vars   map[string]map[string]string
	Groups []env.GroupInfo
}

// textSnap is the in-memory text vault view shared by the text tab and any
// other consumer needing groups + entry metadata. Values (content) are never
// retained — the tab renders metadata only.
type textSnap struct {
	Groups []text.GroupInfo
	Items  map[string][]text.TextInfo
}

// snapshotRegistry holds the latest env/text snapshots, rebuilt under a mutex
// (single-flight) and atomically replaced. A nil registry falls back to a
// direct Manager.Snapshot call so tests that skip New() still work.
type snapshotRegistry struct {
	mu       sync.Mutex
	env      *env.Manager
	text     *text.Manager
	envSnap  *envSnap
	textSnap *textSnap
}

func newSnapshotRegistry(envMgr *env.Manager, textMgr *text.Manager) *snapshotRegistry {
	if envMgr == nil && textMgr == nil {
		return nil
	}
	return &snapshotRegistry{env: envMgr, text: textMgr}
}

func (r *snapshotRegistry) Get() (*envSnap, error) {
	if r == nil || r.env == nil {
		return nil, fmt.Errorf("env manager unavailable")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.envSnap != nil {
		return r.envSnap, nil
	}
	vars, groups, err := r.env.Snapshot()
	if err != nil {
		return nil, err
	}
	r.envSnap = &envSnap{Vars: vars, Groups: groups}
	return r.envSnap, nil
}

// GetText 返回 text 聚合快照（single-flight，与 Get 同一失效语义）。
func (r *snapshotRegistry) GetText() (*textSnap, error) {
	if r == nil || r.text == nil {
		return nil, fmt.Errorf("text manager unavailable")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.textSnap != nil {
		return r.textSnap, nil
	}
	snap, err := r.text.Snapshot()
	if err != nil {
		return nil, err
	}
	r.textSnap = &textSnap{Groups: snap.Groups, Items: snap.Items}
	return r.textSnap, nil
}

// Invalidate 同时作废 env 与 text 快照：写操作与 pull 应用后由 model 调用。
func (r *snapshotRegistry) Invalidate() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.envSnap = nil
	r.textSnap = nil
	r.mu.Unlock()
}

// SeedFromCache 用快照缓存数据预热 memo（D4 首屏即时的前提）：首个 tab
// load 命中 memo 立即渲染，真实解密在后台 VerifyCache 比对后进行。
func (r *snapshotRegistry) SeedFromCache(env *envSnap, text *textSnap) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if env != nil {
		r.envSnap = env
	}
	if text != nil {
		r.textSnap = text
	}
}

// VerifyCache 是缓存基线的后台校验：绕开 memo 用真实解密重建 env/text
// 视图并与缓存逐域比对。一致返回 false（memo 不动）；不一致则原子替换为
// 真实数据并返回 true，由调用方重载已激活 tab。真实读取失败保持缓存数据
// （静默；后续 Invalidate 后由常规路径纠正）。
func (r *snapshotRegistry) VerifyCache() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	if r.env != nil && r.envSnap != nil {
		if vars, groups, err := r.env.Snapshot(); err == nil {
			fresh := &envSnap{Vars: vars, Groups: groups}
			if !reflect.DeepEqual(r.envSnap, fresh) {
				r.envSnap = fresh
				changed = true
			}
		}
	}
	if r.text != nil && r.textSnap != nil {
		if snap, err := r.text.Snapshot(); err == nil {
			fresh := &textSnap{Groups: snap.Groups, Items: snap.Items}
			if !reflect.DeepEqual(r.textSnap, fresh) {
				r.textSnap = fresh
				changed = true
			}
		}
	}
	return changed
}

func envSnapshot(mgr Managers) (map[string]map[string]string, []env.GroupInfo, error) {
	if mgr.snap != nil {
		snap, err := mgr.snap.Get()
		if err != nil {
			return nil, nil, err
		}
		return snap.Vars, snap.Groups, nil
	}
	if mgr.Env == nil {
		return nil, nil, fmt.Errorf("env manager unavailable")
	}
	return mgr.Env.Snapshot()
}

// textSnapshot 取 text 聚合快照：有 registry 走进程内 memo（与 env 同机制），
// 无 registry（测试跳过 New）直接调 Manager.Snapshot。
func textSnapshot(mgr Managers) (*text.Snapshot, error) {
	if mgr.snap != nil {
		snap, err := mgr.snap.GetText()
		if err != nil {
			return nil, err
		}
		return &text.Snapshot{Groups: snap.Groups, Items: snap.Items}, nil
	}
	if mgr.Text == nil {
		return nil, fmt.Errorf("text manager unavailable")
	}
	return mgr.Text.Snapshot()
}
