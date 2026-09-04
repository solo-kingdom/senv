package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingServer 包装 serverAPI 并统计 pull/push 次数（验证"零网络"断言）
type countingServer struct {
	serverAPI
	mu     sync.Mutex
	pulls  int
	pushes int
}

func (c *countingServer) Pull(ctx context.Context, vault string, since int64) ([]Entry, int64, error) {
	c.mu.Lock()
	c.pulls++
	c.mu.Unlock()
	return c.serverAPI.Pull(ctx, vault, since)
}

func (c *countingServer) Push(ctx context.Context, vault string, entries []Entry) ([]Entry, int64, error) {
	c.mu.Lock()
	c.pushes++
	c.mu.Unlock()
	return c.serverAPI.Push(ctx, vault, entries)
}

func (c *countingServer) counts() (pulls, pushes int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pulls, c.pushes
}

// blockingServer 在 Pull 时阻塞直到 ctx 取消，模拟慢网络
type blockingServer struct {
	serverAPI
	gate chan struct{}
}

func (b *blockingServer) Pull(ctx context.Context, vault string, since int64) ([]Entry, int64, error) {
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case <-b.gate:
	}
	return b.serverAPI.Pull(ctx, vault, since)
}

// newAutoProvider 与 newTestProvider 相同，但允许注入任意 serverAPI
func newAutoProvider(t *testing.T, srv serverAPI) (*ServerProvider, *localCache) {
	t.Helper()
	configPath, dataPath := t.TempDir(), t.TempDir()
	fs := newFakeServer()
	fs.metadata["main"] = []byte(`{"salt":"x","password_key":"y"}`)
	p := newServerProvider(fs, configPath, dataPath, "main")
	if err := p.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	return p, p.cache
}

func TestAutoPullThrottledZeroNetwork(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)

	// 远端有新条目，但节流窗口刚被 Bootstrap 占用（last_pull_at 为 0 时先拉一次建立基准）
	if _, _, err := p.AutoPull(context.Background(), 30*time.Second, false); err != nil {
		t.Fatalf("first AutoPull: %v", err)
	}
	cs := &countingServer{serverAPI: srv}
	p.api = cs
	srv.Push(context.Background(), "main", []Entry{{Kind: KindEnv, Grp: "default", Key: "R1", Ciphertext: []byte("r"), BaseRevision: 0}})

	res, skip, err := p.AutoPull(context.Background(), 30*time.Second, false)
	if err != nil {
		t.Fatalf("AutoPull: %v", err)
	}
	if skip != AutoSyncSkipThrottled || res != nil {
		t.Fatalf("skip = %q res = %+v, want throttled/nil", skip, res)
	}
	if pulls, _ := cs.counts(); pulls != 0 {
		t.Errorf("throttled AutoPull made %d network pulls, want 0", pulls)
	}

	// 窗口过期后拉取生效
	st, _ := cache.loadState()
	st.LastPullAt = p.now().Add(-time.Minute).Unix()
	_ = cache.saveState(st)
	res, skip, err = p.AutoPull(context.Background(), 30*time.Second, false)
	if err != nil || skip != AutoSyncRan {
		t.Fatalf("AutoPull after window: skip=%q err=%v", skip, err)
	}
	if res.Applied != 1 {
		t.Errorf("applied = %d, want 1", res.Applied)
	}
	if _, err := os.Stat(mustEntryPath(t, cache, KindEnv, "default", "R1")); err != nil {
		t.Errorf("remote entry not applied: %v", err)
	}
}

func TestAutoPullRefreshBypassesThrottle(t *testing.T) {
	srv := newFakeServer()
	p, _ := newTestProvider(t, srv)
	if _, _, err := p.AutoPull(context.Background(), time.Hour, false); err != nil {
		t.Fatalf("first AutoPull: %v", err)
	}
	cs := &countingServer{serverAPI: srv}
	p.api = cs
	// 窗口极长，但 refresh 绕过
	if _, skip, err := p.AutoPull(context.Background(), time.Hour, true); err != nil || skip != AutoSyncRan {
		t.Fatalf("refresh AutoPull: skip=%q err=%v", skip, err)
	}
	if pulls, _ := cs.counts(); pulls != 1 {
		t.Errorf("refresh made %d pulls, want 1", pulls)
	}
}

func TestAutoPullTimeoutDegradation(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	if _, _, err := p.AutoPull(context.Background(), time.Second, false); err != nil {
		t.Fatalf("first AutoPull: %v", err)
	}
	stBefore, _ := cache.loadState()

	// 慢网络：ctx 超时后返回错误，last_pull_at 不更新（下次仍会尝试）
	bs := &blockingServer{serverAPI: srv, gate: make(chan struct{})}
	p.api = bs
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := p.AutoPull(ctx, 0, false)
	if err == nil {
		t.Fatal("timed-out AutoPull should return error for caller to swallow")
	}
	stAfter, _ := cache.loadState()
	if stAfter.LastPullAt != stBefore.LastPullAt {
		t.Error("failed pull must not update last_pull_at")
	}
	close(bs.gate)
}

func TestAutoPullLockedSkip(t *testing.T) {
	srv := newFakeServer()
	p, _ := newTestProvider(t, srv)
	if _, _, err := p.AutoPull(context.Background(), time.Second, false); err != nil {
		t.Fatalf("first AutoPull: %v", err)
	}
	cs := &countingServer{serverAPI: srv}
	p.api = cs

	// 模拟另一进程持锁
	held, err := acquireSyncLock(p.cache.dataPath, false)
	if err != nil {
		t.Fatalf("hold lock: %v", err)
	}
	defer held.release()

	_, skip, err := p.AutoPull(context.Background(), 0, false)
	if err != nil || skip != AutoSyncSkipLocked {
		t.Fatalf("skip=%q err=%v, want locked/nil", skip, err)
	}
	if pulls, _ := cs.counts(); pulls != 0 {
		t.Errorf("locked AutoPull made %d pulls, want 0", pulls)
	}
}

func TestAutoPushCleanSkipZeroNetwork(t *testing.T) {
	srv := newFakeServer()
	p, _ := newTestProvider(t, srv)
	if _, _, err := p.AutoPull(context.Background(), 0, false); err != nil {
		t.Fatalf("AutoPull: %v", err)
	}
	cs := &countingServer{serverAPI: srv}
	p.api = cs
	out, err := p.AutoPush(context.Background(), autoSyncBudget)
	if err != nil {
		t.Fatalf("AutoPush: %v", err)
	}
	if out.Skip != AutoSyncSkipClean || out.Dirty != 0 {
		t.Fatalf("outcome = %+v, want clean/0", out)
	}
	if _, pushes := cs.counts(); pushes != 0 {
		t.Errorf("clean AutoPush made %d pushes, want 0", pushes)
	}
}

func TestAutoPushNetworkErrorKeepsDirty(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	if _, _, err := p.AutoPull(context.Background(), 0, false); err != nil {
		t.Fatalf("AutoPull: %v", err)
	}
	writeEnvVar(t, cache, "default", "K", "v")

	srv.fail = true
	out, err := p.AutoPush(context.Background(), autoSyncBudget)
	if err == nil {
		t.Fatal("offline AutoPush should return error for caller to warn")
	}
	if out.Dirty != 1 {
		t.Errorf("Dirty = %d, want 1 (kept for warning)", out.Dirty)
	}
	// 本地数据与 dirty 状态保留：恢复后重试成功
	srv.fail = false
	out, err = p.AutoPush(context.Background(), autoSyncBudget)
	if err != nil || out.Pushed != 1 {
		t.Fatalf("retry after recovery: out=%+v err=%v", out, err)
	}
}

func TestAutoPushConflictKeepsDirty(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()
	if _, _, err := p.AutoPull(ctx, 0, false); err != nil {
		t.Fatalf("AutoPull: %v", err)
	}
	writeEnvVar(t, cache, "default", "A", "v1")
	if _, err := p.AutoPush(ctx, autoSyncBudget); err != nil {
		t.Fatalf("initial push: %v", err)
	}

	// 远端更新 A；本地也修改 → AutoPush 冲突
	srv.Push(ctx, "main", []Entry{{Kind: KindEnv, Grp: "default", Key: "A", Ciphertext: []byte("remote-v2"), BaseRevision: 1}})
	writeEnvVar(t, cache, "default", "A", "local-v2")
	out, err := p.AutoPush(ctx, autoSyncBudget)
	var conflictErr *SyncConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("AutoPush error = %v, want SyncConflictError", err)
	}
	if out.Dirty != 1 {
		t.Errorf("Dirty = %d, want 1", out.Dirty)
	}
	// 两端数据均未改动
	if got := srv.entries["main"][entryID(KindEnv, "default", "A")].Ciphertext; string(got) != "remote-v2" {
		t.Errorf("server entry changed to %q", got)
	}
	if data, _ := os.ReadFile(mustEntryPath(t, cache, KindEnv, "default", "A")); string(data) != "local-v2" {
		t.Errorf("local entry changed to %q", data)
	}
}

func TestAutoPushLockedSkip(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	if _, _, err := p.AutoPull(context.Background(), 0, false); err != nil {
		t.Fatalf("AutoPull: %v", err)
	}
	writeEnvVar(t, cache, "default", "K", "v")

	held, err := acquireSyncLock(p.cache.dataPath, false)
	if err != nil {
		t.Fatalf("hold lock: %v", err)
	}
	defer held.release()

	cs := &countingServer{serverAPI: srv}
	p.api = cs
	out, err := p.AutoPush(context.Background(), autoSyncBudget)
	if err != nil || out.Skip != AutoSyncSkipLocked {
		t.Fatalf("out=%+v err=%v, want locked/nil", out, err)
	}
	if _, pushes := cs.counts(); pushes != 0 {
		t.Errorf("locked AutoPush made %d pushes, want 0", pushes)
	}
}

func TestPushBlocking(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	if _, _, err := p.AutoPull(context.Background(), 0, false); err != nil {
		t.Fatalf("AutoPull: %v", err)
	}
	writeEnvVar(t, cache, "default", "PW", "v")
	res, err := p.PushBlocking(context.Background())
	if err != nil || res.Pushed != 1 {
		t.Fatalf("PushBlocking = %+v err=%v, want pushed=1", res, err)
	}
	if _, ok := srv.entries["main"][entryID(KindEnv, "default", "PW")]; !ok {
		t.Error("blocking push did not reach server")
	}

	srv.fail = true
	writeEnvVar(t, cache, "default", "PW", "v2")
	if _, err := p.PushBlocking(context.Background()); err == nil {
		t.Fatal("PushBlocking on offline server should return error")
	}
}

func TestSyncLockPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("flock unavailable on windows")
	}
	dataPath := t.TempDir()
	lock, err := acquireSyncLock(dataPath, false)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	info, err := os.Stat(filepath.Join(dataPath, syncLockFileName))
	if err != nil {
		t.Fatalf("stat lock file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("lock file perm = %o, want 600", perm)
	}
	di, err := os.Stat(dataPath)
	if err != nil {
		t.Fatalf("stat data dir: %v", err)
	}
	if dp := di.Mode().Perm(); dp != 0o700 {
		t.Errorf("data dir perm = %o, want 700", dp)
	}
	// 非阻塞二次获取冲突；阻塞获取在释放后成功
	if _, err := acquireSyncLock(dataPath, false); !errors.Is(err, errSyncLocked) {
		t.Errorf("second acquire = %v, want errSyncLocked", err)
	}
	if err := lock.release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	lock2, err := acquireSyncLock(dataPath, true)
	if err != nil {
		t.Fatalf("blocking acquire after release: %v", err)
	}
	_ = lock2.release()
}

func TestSyncStateBackwardCompat(t *testing.T) {
	// 旧格式（无 last_pull_at 字段）可正常加载且零值 = 立即拉取
	dataPath := t.TempDir()
	old := `{"last_synced_revision": 7, "metadata_hash": "abc", "entries": {}}`
	if err := os.WriteFile(filepath.Join(dataPath, syncStateFileName), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &localCache{configPath: t.TempDir(), dataPath: dataPath}
	st, err := c.loadState()
	if err != nil {
		t.Fatalf("load old state: %v", err)
	}
	if st.LastSyncedRevision != 7 || st.LastPullAt != 0 {
		t.Errorf("state = %+v, want revision 7 / last_pull_at 0", st)
	}
}

func TestCorruptStateSkipsAutoSync(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	if err := os.WriteFile(filepath.Join(cache.dataPath, syncStateFileName), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.AutoPull(context.Background(), 0, false); err == nil {
		t.Error("AutoPull on corrupt state should return error (caller skips silently)")
	}
	if _, err := p.AutoPush(context.Background(), autoSyncBudget); err == nil {
		t.Error("AutoPush on corrupt state should return error")
	}
	// 手动同步明确报错（含指引）
	if _, err := p.SyncWithReport(context.Background()); err == nil {
		t.Error("manual sync on corrupt state should surface error")
	}
}

func TestAutoSyncTwoMachinesConverge(t *testing.T) {
	srv := newFakeServer()
	srv.metadata["main"] = []byte(`{"salt":"x","password_key":"y"}`)
	ctx := context.Background()

	machineA := newServerProvider(srv, t.TempDir(), t.TempDir(), "main")
	if err := machineA.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	writeEnvVar(t, machineA.cache, "default", "A_KEY", "from-a")
	if _, err := machineA.AutoPush(ctx, autoSyncBudget); err != nil {
		t.Fatalf("machine A push: %v", err)
	}

	machineB := newServerProvider(srv, t.TempDir(), t.TempDir(), "main")
	if err := machineB.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	// Simulate a post-bootstrap remote update by resetting B's cursor/timestamp.
	stB, err := machineB.cache.loadState()
	if err != nil {
		t.Fatal(err)
	}
	stB.LastSyncedRevision = 0
	stB.LastPullAt = 1
	if err := machineB.cache.saveState(stB); err != nil {
		t.Fatal(err)
	}
	if _, _, err := machineB.AutoPull(ctx, time.Hour, false); err != nil {
		t.Fatalf("machine B pull: %v", err)
	}
	if data, err := os.ReadFile(mustEntryPath(t, machineB.cache, KindEnv, "default", "A_KEY")); err != nil || string(data) != "from-a" {
		t.Fatalf("machine B A_KEY = %q,%v; want from-a", data, err)
	}
	writeEnvVar(t, machineB.cache, "default", "B_KEY", "from-b")
	if _, err := machineB.AutoPush(ctx, autoSyncBudget); err != nil {
		t.Fatalf("machine B push: %v", err)
	}

	if _, _, err := machineA.AutoPull(ctx, 0, true); err != nil {
		t.Fatalf("machine A pull: %v", err)
	}
	writeEnvVar(t, machineA.cache, "default", "A_KEY2", "from-a-2")
	if _, err := machineA.AutoPush(ctx, autoSyncBudget); err != nil {
		t.Fatalf("machine A second push: %v", err)
	}
	stB, err = machineB.cache.loadState()
	if err != nil {
		t.Fatal(err)
	}
	stB.LastSyncedRevision = 0
	stB.LastPullAt = 1
	if err := machineB.cache.saveState(stB); err != nil {
		t.Fatal(err)
	}
	if _, _, err := machineB.AutoPull(ctx, time.Hour, false); err != nil {
		t.Fatalf("machine B final pull: %v", err)
	}
	stA, err := machineA.cache.loadState()
	if err != nil {
		t.Fatal(err)
	}
	stB, err = machineB.cache.loadState()
	if err != nil {
		t.Fatal(err)
	}
	if stA.LastSyncedRevision != stB.LastSyncedRevision {
		t.Fatalf("last_synced_revision A=%d B=%d", stA.LastSyncedRevision, stB.LastSyncedRevision)
	}
	for _, key := range []string{"A_KEY", "A_KEY2", "B_KEY"} {
		_, errA := os.Stat(mustEntryPath(t, machineA.cache, KindEnv, "default", key))
		_, errB := os.Stat(mustEntryPath(t, machineB.cache, KindEnv, "default", key))
		if errA != nil || errB != nil {
			t.Fatalf("key %s missing: A=%v B=%v", key, errA, errB)
		}
	}
}

func TestAutoSyncOfflineBacklogDrainsAfterRecovery(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()
	writeEnvVar(t, cache, "default", "OFFLINE", "value")

	srv.fail = true
	if out, err := p.AutoPush(ctx, autoSyncBudget); err == nil || out.Dirty != 1 {
		t.Fatalf("offline AutoPush = %+v,%v; want dirty=1/error", out, err)
	}
	if _, err := os.Stat(mustEntryPath(t, cache, KindEnv, "default", "OFFLINE")); err != nil {
		t.Fatalf("offline write was not retained: %v", err)
	}

	srv.fail = false
	out, err := p.AutoPush(ctx, autoSyncBudget)
	if err != nil || out.Pushed != 1 {
		t.Fatalf("recovered AutoPush = %+v,%v; want pushed=1", out, err)
	}
}

func TestAutoSyncConcurrentWritesKeepStateConsistent(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	const writers = 8

	// Establish a clean state snapshot before concurrent writes.
	if _, err := p.SyncWithReport(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 预置 config 子系统数据：并发风暴后状态不得丢失其快照与 metadata 哈希。
	indexPath := filepath.Join(cache.configPath, "config_index.json")
	if err := os.WriteFile(indexPath, []byte(`{"configs":{"db":{"name":"db","encrypted_file":"db.pem.enc"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AutoPush(context.Background(), autoSyncBudget); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < writers; i++ {
		writeEnvVar(t, cache, "default", fmt.Sprintf("K_%d", i), fmt.Sprintf("v%d", i))
	}

	var wg sync.WaitGroup
	var failures atomic.Int32
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker := newServerProvider(srv, p.cache.configPath, p.cache.dataPath, "main")
			if _, err := worker.AutoPush(context.Background(), autoSyncBudget); err != nil {
				t.Errorf("concurrent AutoPush: %v", err)
				failures.Add(1)
			}
		}()
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("%d concurrent pushes failed", failures.Load())
	}

	var st syncState
	data, err := os.ReadFile(filepath.Join(cache.dataPath, syncStateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("state JSON is corrupt: %v", err)
	}
	for i := 0; i < writers; i++ {
		key := fmt.Sprintf("K_%d", i)
		entry, ok := srv.entries["main"][entryID(KindEnv, "default", key)]
		if !ok {
			t.Fatalf("entry %s missing on server", key)
		}
		snap, ok := st.Entries[entryID(KindEnv, "default", key)]
		if !ok || snap.Revision != entry.Revision {
			t.Fatalf("state snapshot for %s = %+v, entry revision %d", key, snap, entry.Revision)
		}
	}
	indexSnap, ok := st.Entries[entryID(KindConfigIndex, "", "")]
	if !ok || indexSnap.Revision == 0 {
		t.Fatalf("config_index snapshot lost after concurrent pushes: %+v", indexSnap)
	}
	if st.MetadataHash == "" {
		t.Fatal("metadata hash cleared after concurrent pushes")
	}
}
