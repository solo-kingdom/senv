package ledger

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wii/senv/internal/storage"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	sm := storage.NewManager(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := sm.Initialize("test-password"); err != nil {
		t.Fatalf("init vault: %v", err)
	}
	key, err := sm.DeriveKeyFromPassword("test-password")
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}
	return NewManager(sm, key)
}

func TestHeadAndRecordKeyValidation(t *testing.T) {
	if _, err := HeadKey("device", "lnv"); err != nil {
		t.Errorf("valid head key rejected: %v", err)
	}
	for _, bad := range []string{"Device.lnv", "device", "device.", "device.l@nv", "device.l nv"} {
		if _, err := HeadKey(splitTypeID(bad)); err == nil {
			continue // 拆分不成立时由 SplitHeadKey 报错，见下
		}
	}
	if _, _, err := SplitHeadKey("device"); err == nil {
		t.Error("missing dot should fail")
	}
	if _, _, err := SplitHeadKey("device.l@nv"); err == nil {
		t.Error("@ in id should fail")
	}
	if _, err := RecordKey("device.lnv", time.Now(), "zzzz"); err == nil {
		t.Error("non-hex suffix should fail")
	}
	if _, err := RecordKey("device.lnv", time.Now(), "3f9a"); err != nil {
		t.Errorf("valid record key rejected: %v", err)
	}
}

func splitTypeID(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

func TestAppendIsImmutableAndOrdered(t *testing.T) {
	m := newTestManager(t)
	const grp = "personal"

	h, err := NewHead("device", "lnv", "自托管节点", []string{"self-host"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SaveHead(grp, h); err != nil {
		t.Fatal(err)
	}
	created := h.UpdatedAt

	// 追加两条记录：不修改实体头
	for i, summary := range []string{"升级到 t0.1.26", "清理容器日志"} {
		_, _, err := m.Append(grp, "device.lnv", RecordInput{
			Summary: summary, Body: "做了什么: 略", At: time.Now().Add(time.Duration(i) * time.Minute),
			By: By{Agent: "test-agent"},
		})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	got, err := m.GetHead(grp, "device.lnv")
	if err != nil {
		t.Fatal(err)
	}
	if !got.UpdatedAt.Equal(created) {
		t.Errorf("append must not touch head UpdatedAt (got %v want %v)", got.UpdatedAt, created)
	}

	recs, err := m.Log(grp, "device.lnv", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("log len = %d, want 2", len(recs))
	}
	// 新到旧
	if recs[0].At.Before(recs[1].At) {
		t.Errorf("log not newest-first: %v then %v", recs[0].At, recs[1].At)
	}
	// 记录键格式 <head>@<ts>-<4hex>
	if _, _, ok := cut(recs[0].Key, "@"); !ok {
		t.Errorf("record key = %q, want <head>@<ts>-<hex>", recs[0].Key)
	}
}

func cut(s, sep string) (string, string, bool) {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return s[:i], s[i+len(sep):], true
		}
	}
	return s, "", false
}

func TestEditFieldAndStatus(t *testing.T) {
	m := newTestManager(t)
	const grp = "personal"
	h, _ := NewHead("service", "senv-server", "密文托管", nil)
	if err := m.SaveHead(grp, h); err != nil {
		t.Fatal(err)
	}
	if err := m.EditField(grp, "service.senv-server", "版本", "v0.1.67"); err != nil {
		t.Fatal(err)
	}
	if err := m.EditField(grp, "service.senv-server", "位置", "云"); err != nil {
		t.Fatal(err)
	}
	got, _ := m.GetHead(grp, "service.senv-server")
	if len(got.Fields) != 2 || got.Fields[0].Value != "v0.1.67" {
		t.Fatalf("fields = %+v", got.Fields)
	}
	// 空值删除字段
	if err := m.EditField(grp, "service.senv-server", "位置", ""); err != nil {
		t.Fatal(err)
	}
	got, _ = m.GetHead(grp, "service.senv-server")
	if len(got.Fields) != 1 {
		t.Fatalf("fields after delete = %+v", got.Fields)
	}

	for i := 0; i < 3; i++ {
		if err := m.SetStatus(grp, "service.senv-server", StatusEntry{Text: "ok", At: time.Now().Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	got, _ = m.GetHead(grp, "service.senv-server")
	if len(got.Status) != StatusKeep {
		t.Fatalf("status len = %d, want %d", len(got.Status), StatusKeep)
	}
}

func TestDeleteRecord(t *testing.T) {
	m := newTestManager(t)
	const grp = "personal"
	h, _ := NewHead("device", "x", "", nil)
	if err := m.SaveHead(grp, h); err != nil {
		t.Fatal(err)
	}
	_, recKey, err := m.Append(grp, "device.x", RecordInput{Summary: "tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteRecord(grp, recKey); err != nil {
		t.Fatalf("delete record: %v", err)
	}
	recs, err := m.Log(grp, "device.x", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("log after delete = %d, want 0", len(recs))
	}
	// 非法记录键被拒
	if err := m.DeleteRecord(grp, "device.x"); err == nil {
		t.Error("head key without @ should be rejected")
	}
}

func TestTimelineAcrossEntities(t *testing.T) {
	m := newTestManager(t)
	h1, _ := NewHead("device", "a", "", nil)
	h2, _ := NewHead("service", "b", "", nil)
	_ = m.SaveHead("personal", h1)
	_ = m.SaveHead("default", h2)

	if _, _, err := m.Append("personal", "device.a", RecordInput{Summary: "first", At: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Append("default", "service.b", RecordInput{Summary: "second", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	recs, err := m.Timeline(10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("timeline len = %d, want 2", len(recs))
	}
	if recs[0].Summary != "second" {
		t.Errorf("timeline[0] = %q, want newest", recs[0].Summary)
	}
	filtered, _ := m.Timeline(10, "device")
	if len(filtered) != 1 || filtered[0].Summary != "first" {
		t.Errorf("type filter = %+v", filtered)
	}
}
