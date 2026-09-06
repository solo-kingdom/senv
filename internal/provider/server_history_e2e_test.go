package provider

import (
	"context"
	"testing"
	"time"

	"github.com/wii/senv/internal/storage"
)

// TestE2EEntryHistoryAndRestore 端到端：修改产生历史 → 查询（含日期）→
// 恢复旧值产生新 revision → 删除后从历史找回。
func TestE2EEntryHistoryAndRestore(t *testing.T) {
	baseURL, token, _ := e2eEnv(t)
	ctx := context.Background()
	password := "e2e-history-password"

	cfg, data, key := newLocalVault(t, password)
	p := NewServerProvider(baseURL, token, cfg, data, "main")
	if _, err := p.SyncWithReport(ctx); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	sm := storage.NewManager(cfg, data)
	setVar := func(value string) {
		if err := sm.SaveEnvVarWithKey("deploy", "KEY", &storage.EnvVarEntry{
			Value: value, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}, key); err != nil {
			t.Fatalf("save env %q: %v", value, err)
		}
		if _, err := p.SyncWithReport(ctx); err != nil {
			t.Fatalf("sync after save %q: %v", value, err)
		}
	}

	setVar("v1")
	setVar("v2")
	setVar("v3")

	// 历史查询：两次修改各留一版，revision 新到旧
	history, err := p.History(ctx, HistoryFilter{Kind: "env", Grp: "deploy", Key: "KEY"})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2", len(history))
	}
	if history[0].Revision < history[1].Revision {
		t.Errorf("history not ordered by revision DESC: %d then %d", history[0].Revision, history[1].Revision)
	}
	if history[0].CreatedAt.IsZero() {
		t.Error("history entries should carry server timestamps")
	}

	// 恢复到 v2（history[0] 是 v3 修改前的值）→ 本地值变回，产生新 revision
	if err := p.RestoreEntry(ctx, "env", "deploy", "KEY", history[0].Ciphertext); err != nil {
		t.Fatalf("RestoreEntry: %v", err)
	}
	entry, err := sm.LoadEnvVarWithKey("deploy", "KEY", key)
	if err != nil || entry.Value != "v2" {
		t.Fatalf("after restore value = %q, %v; want v2", entry.Value, err)
	}

	// 删除 → 历史保留最后密文 → 从历史找回
	if err := sm.DeleteEnvVar("deploy", "KEY"); err != nil {
		t.Fatalf("delete env: %v", err)
	}
	if _, err := p.SyncWithReport(ctx); err != nil {
		t.Fatalf("sync after delete: %v", err)
	}
	history, err = p.History(ctx, HistoryFilter{Kind: "env", Grp: "deploy", Key: "KEY"})
	if err != nil {
		t.Fatalf("History after delete: %v", err)
	}
	if len(history) < 3 {
		t.Fatalf("history after delete = %d, want at least 3 (含删除前像)", len(history))
	}
	// 找回删除前的最后值 v2：history[0] 是删除前像
	if err := p.RestoreEntry(ctx, "env", "deploy", "KEY", history[0].Ciphertext); err != nil {
		t.Fatalf("RestoreEntry deleted: %v", err)
	}
	entry, err = sm.LoadEnvVarWithKey("deploy", "KEY", key)
	if err != nil || entry.Value != "v2" {
		t.Fatalf("after recover value = %q, %v; want v2", entry.Value, err)
	}

	// 恢复后的条目与远端一致：再次 sync 无冲突
	if _, err := p.SyncWithReport(ctx); err != nil {
		t.Errorf("sync after restore should be conflict-free: %v", err)
	}
}

// TestHistoryUnsupportedOnFake 验证不支持历史的 api 返回明确错误
func TestHistoryUnsupportedOnFake(t *testing.T) {
	p := newServerProvider(&fakeHistoryLessAPI{}, t.TempDir(), t.TempDir(), "main")
	if _, err := p.History(context.Background(), HistoryFilter{}); err != ErrHistoryUnsupported {
		t.Fatalf("err = %v, want ErrHistoryUnsupported", err)
	}
}

type fakeHistoryLessAPI struct{ serverAPI }

func (f *fakeHistoryLessAPI) GetMetadata(ctx context.Context, vault string) ([]byte, error) {
	return nil, nil
}
