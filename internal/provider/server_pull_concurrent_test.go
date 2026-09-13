package provider

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

// gatedPullServer 包装 serverAPI：Pull 进入网络阶段时发信号并阻塞，直到测试
// 放行（或 ctx 取消），让「pull 正在网络阶段」成为测试可观察、可编排的状态。
type gatedPullServer struct {
	serverAPI
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *gatedPullServer) Pull(ctx context.Context, vault string, since int64) ([]Entry, int64, error) {
	g.once.Do(func() { close(g.entered) })
	select {
	case <-g.release:
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
	return g.serverAPI.Pull(ctx, vault, since)
}

// waitTimeout 在 dur 内等待 ch，失败返回 false（避免测试永久挂起）。
func waitTimeout(t *testing.T, ch <-chan struct{}, dur time.Duration, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(dur):
		t.Fatalf("timeout waiting for %s after %s", what, dur)
	}
}

// TestPullNetworkPhaseDoesNotBlockVaultMutation 验证拆锁后 pull 的网络阶段
// 不持有 vault 排它锁：网络挂起期间，本地写（vault mutation 锁路径，与 TUI
// 读取同一把锁）必须立即完成，而不是排队到 pull 结束。
func TestPullNetworkPhaseDoesNotBlockVaultMutation(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()
	// 建立基线：远端条目 R 经一次 pull 落盘并写入快照
	srv.Push(ctx, "main", []Entry{{Kind: KindEnv, Grp: "default", Key: "R", Ciphertext: []byte("r"), BaseRevision: 0}})
	if _, err := p.pull(ctx); err != nil {
		t.Fatalf("baseline pull: %v", err)
	}

	// 远端再更新 R；注入可编排的慢网络
	srv.Push(ctx, "main", []Entry{{Kind: KindEnv, Grp: "default", Key: "R", Ciphertext: []byte("r2"), BaseRevision: 1}})
	gate := &gatedPullServer{serverAPI: srv, entered: make(chan struct{}), release: make(chan struct{})}
	p.api = gate

	pullDone := make(chan error, 1)
	go func() {
		_, err := p.pull(ctx)
		pullDone <- err
	}()
	waitTimeout(t, gate.entered, 5*time.Second, "pull network phase")

	// 网络阶段挂起期间发起本地写（与 TUI 加载同一把 vault 排它锁）
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- p.withVaultMutation(func() error {
			writeEnvVar(t, cache, "default", "W", "local-during-pull")
			return nil
		})
	}()
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("local write during pull network phase: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("local write blocked: pull network phase still holds the vault lock")
	}

	close(gate.release)
	if err := <-pullDone; err != nil {
		t.Fatalf("pull: %v", err)
	}
	// 远端更新已应用，窗口内本地写未被触碰
	assertTestFile(t, mustEntryPath(t, cache, KindEnv, "default", "R"), []byte("r2"))
	assertTestFile(t, mustEntryPath(t, cache, KindEnv, "default", "W"), []byte("local-during-pull"))
}

// TestPullWriteDuringNetworkWindowKeepsDirtySemantics 验证网络窗口内落入的
// 本地写不被 pull 应用覆盖（dirty 队列语义不变）：apply 阶段重算 dirty，
// 窗口内变 dirty 的条目被跳过并进入 RemoteCandidates，后续 push 走乐观锁
// 报冲突而不是静默覆盖任一侧。
func TestPullWriteDuringNetworkWindowKeepsDirtySemantics(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()

	// A 两端一致（rev1 已入快照）
	writeEnvVar(t, cache, "default", "A", "v1")
	if _, err := p.SyncWithReport(ctx); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	// 远端更新 A（另一台机器）
	srv.Push(ctx, "main", []Entry{{Kind: KindEnv, Grp: "default", Key: "A", Ciphertext: []byte("remote-v2"), BaseRevision: 1}})

	gate := &gatedPullServer{serverAPI: srv, entered: make(chan struct{}), release: make(chan struct{})}
	p.api = gate
	resCh := make(chan *PullResult, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := p.pull(ctx)
		resCh <- res
		errCh <- err
	}()
	waitTimeout(t, gate.entered, 5*time.Second, "pull network phase")

	// 网络窗口内本地修改 A → 应用阶段必须视为 dirty 而跳过
	writeEnvVar(t, cache, "default", "A", "local-v2")
	close(gate.release)
	if err := <-errCh; err != nil {
		t.Fatalf("pull: %v", err)
	}
	res := <-resCh
	if res.SkippedDirty != 1 {
		t.Fatalf("SkippedDirty = %d, want 1", res.SkippedDirty)
	}
	if len(res.RemoteCandidates) != 1 || string(res.RemoteCandidates[0].Ciphertext) != "remote-v2" {
		t.Fatalf("RemoteCandidates = %+v, want remote-v2", res.RemoteCandidates)
	}
	assertTestFile(t, mustEntryPath(t, cache, KindEnv, "default", "A"), []byte("local-v2"))

	// dirty 语义不变：本地 A（base=快照 rev1）对远端 rev2 推送 → 乐观锁冲突，
	// 两侧数据均未改动
	_, err := p.push(ctx, res.RemoteCandidates...)
	var conflictErr *SyncConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("push after window write = %v, want SyncConflictError", err)
	}
	assertTestFile(t, mustEntryPath(t, cache, KindEnv, "default", "A"), []byte("local-v2"))
	if got := srv.entries["main"][entryID(KindEnv, "default", "A")].Ciphertext; string(got) != "remote-v2" {
		t.Errorf("server entry changed to %q, want remote-v2", got)
	}
}

// TestPullApplyFailureWithWindowWriteRollsBack 验证应用阶段的原子性：网络窗口
// 内落入的本地写 + 注入的应用阶段故障 → 整体回滚，本地写与同步状态均不受损。
func TestPullApplyFailureWithWindowWriteRollsBack(t *testing.T) {
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()

	writeEnvVar(t, cache, "default", "A", "v1")
	if _, err := p.SyncWithReport(ctx); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	srv.Push(ctx, "main", []Entry{{Kind: KindEnv, Grp: "default", Key: "A", Ciphertext: []byte("remote-v2"), BaseRevision: 1}})

	gate := &gatedPullServer{serverAPI: srv, entered: make(chan struct{}), release: make(chan struct{})}
	p.api = gate
	pullDone := make(chan error, 1)
	go func() {
		_, err := p.pull(ctx)
		pullDone <- err
	}()
	waitTimeout(t, gate.entered, 5*time.Second, "pull network phase")

	writeEnvVar(t, cache, "default", "A", "local-during-pull")
	// 应用阶段注入写故障（状态文件）：应用必须整体回滚
	injectProviderAtomicFault(cache, providerFailWrite, syncStateFileName)
	close(gate.release)
	if err := <-pullDone; !errors.Is(err, errInjectedProviderAtomic) {
		t.Fatalf("pull error = %v, want injected failure", err)
	}
	// 窗口内本地写保留，远端内容未部分落盘，快照 revision 未推进
	assertTestFile(t, mustEntryPath(t, cache, KindEnv, "default", "A"), []byte("local-during-pull"))
	assertStateRevision(t, cache, 1)
	if _, err := os.Stat(mustEntryPath(t, cache, KindEnv, "default", "A")); err != nil {
		t.Fatalf("entry missing after rollback: %v", err)
	}
}
