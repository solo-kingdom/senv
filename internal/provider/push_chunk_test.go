package provider

import (
	"context"
	"errors"
	"testing"
)

// 验证客户端按批推送：服务端单批上限 1000，条目过千时必须分批。
func TestPushChunksUnderBatchLimit(t *testing.T) {
	t.Setenv("SENV_PUSH_BATCH", "3")
	srv := newFakeServer()
	rec := &pushRecorder{serverAPI: srv}
	p, cache := newTestProvider(t, srv)
	p.api = rec

	keys := []string{"A", "B", "C", "D", "E", "F", "G"}
	for _, k := range keys {
		writeEnvVar(t, cache, "default", k, "v1")
	}

	if _, err := p.SyncWithReport(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(rec.sizes) != 3 {
		t.Fatalf("push batches = %v, want 3 batches", rec.sizes)
	}
	for i, n := range rec.sizes {
		if n > 3 {
			t.Errorf("batch %d has %d entries, want <= 3", i, n)
		}
	}
	total := 0
	for _, n := range rec.sizes {
		total += n
	}
	if total != len(keys) {
		t.Errorf("pushed %d entries, want %d", total, len(keys))
	}
	st := readStateFile(t, cache)
	for _, k := range keys {
		if _, ok := st.Entries[entryID(KindEnv, "default", k)]; !ok {
			t.Errorf("entry %s missing snapshot after chunked push", k)
		}
	}
}

func TestPushBatchSizeOverride(t *testing.T) {
	cases := []struct {
		env  string
		want int
	}{
		{"", defaultPushBatchSize},
		{"0", defaultPushBatchSize},
		{"abc", defaultPushBatchSize},
		{"1001", defaultPushBatchSize},
		{"1", 1},
		{"1000", 1000},
	}
	for _, c := range cases {
		t.Setenv("SENV_PUSH_BATCH", c.env)
		if got := pushBatchSize(); got != c.want {
			t.Errorf("pushBatchSize() with %q = %d, want %d", c.env, got, c.want)
		}
	}
}

// 真实冲突出现在后面的批次时：停止后续批次，已推批次状态已持久化。
func TestPushConflictStopsRemainingBatches(t *testing.T) {
	t.Setenv("SENV_PUSH_BATCH", "2")
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)

	for _, k := range []string{"A", "B", "C", "D", "E"} {
		writeEnvVar(t, cache, "default", k, "v1")
	}
	if _, err := p.SyncWithReport(context.Background()); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	// 另一台机器改 E 并推送。
	mb := newServerProvider(srv, t.TempDir(), t.TempDir(), "main")
	if err := mb.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	writeEnvVar(t, mb.cache, "default", "E", "remote")
	if _, err := mb.AutoPush(context.Background(), autoSyncBudget); err != nil {
		t.Fatalf("machine B push: %v", err)
	}

	// 本机改 A-D（快照有效，可正常推送）并把 E 改成与远端不同的值、抹掉快照
	// → 只有 E 是真实冲突，且按标识排序落在最后一批。
	for _, k := range []string{"A", "B", "C", "D"} {
		writeEnvVar(t, cache, "default", k, "v2-local")
	}
	writeEnvVar(t, cache, "default", "E", "local")
	corruptSnapshotFor(t, cache, KindEnv, "default", "E")

	rec := &pushRecorder{serverAPI: srv}
	p.api = rec
	_, err := p.SyncWithReport(context.Background())
	var conflict *SyncConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want SyncConflictError", err)
	}
	found := false
	for _, c := range conflict.Conflicts {
		if c.Key == "E" {
			found = true
		}
	}
	if !found {
		t.Fatalf("conflicts = %+v, want entry E", conflict.Conflicts)
	}
	// 5 个条目、批大小 2：批次应为 2 + 2 + 1，第三批冲突。
	if len(rec.sizes) != 3 {
		t.Errorf("push batches = %v, want 3 attempts (2 ok + 1 conflict)", rec.sizes)
	}
	// 前两批的 A-D 快照已持久化（中途返回不丢进度）。
	st := readStateFile(t, cache)
	for _, k := range []string{"A", "B", "C", "D"} {
		if _, ok := st.Entries[entryID(KindEnv, "default", k)]; !ok {
			t.Errorf("entry %s snapshot not persisted after earlier batches", k)
		}
	}
}

// pushRecorder 记录每批条目数，其余语义转发给 fakeServer（含乐观锁）。
type pushRecorder struct {
	serverAPI
	sizes []int
}

func (r *pushRecorder) Push(ctx context.Context, vault string, entries []Entry) ([]Entry, int64, error) {
	r.sizes = append(r.sizes, len(entries))
	return r.serverAPI.Push(ctx, vault, entries)
}
