package provider

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/server/handler"
	"github.com/wii/senv/internal/server/store"
	"github.com/wii/senv/internal/server/testdb"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

// registrationE2EEnv 搭建真实 server（Postgres + HTTP），返回地址、存量
// user 级 token 与 store 句柄（admin 侧操作用）。
func registrationE2EEnv(t *testing.T) (string, string, *store.Store) {
	t.Helper()
	pool := testdb.New(t)
	st := store.New(pool)
	tok, err := st.CreateUser(context.Background(), "e2e-user")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	srv := httptest.NewServer(handler.New(st))
	t.Cleanup(srv.Close)
	return srv.URL, tok, st
}

// TestE2EClientRegistrationAndBlock 端到端闭环：
// 存量机器建 vault → 一次性注册码注册新 client → 新机器 bootstrap 接入 →
// 管理员屏蔽 → 新机器感知并清理解锁缓存（加密数据保留）→ 解封恢复。
func TestE2EClientRegistrationAndBlock(t *testing.T) {
	baseURL, legacyToken, st := registrationE2EEnv(t)
	ctx := context.Background()
	password := "e2e-password-3"

	// 审计日志写入临时 HOME，便于断言
	t.Setenv("HOME", t.TempDir())

	// 机器 A（存量 user 级 token）：首次同步在 server 端建 vault
	cfgA, dataA, _ := newLocalVault(t, password)
	pA := NewServerProvider(baseURL, legacyToken, cfgA, dataA, "main")
	if _, err := pA.SyncWithReport(ctx); err != nil {
		t.Fatalf("machine A initial sync: %v", err)
	}

	// admin 签发一次性注册码；client 注册换取专属凭证
	userID, err := st.UserIDByName(ctx, "e2e-user")
	if err != nil {
		t.Fatalf("UserIDByName: %v", err)
	}
	code, err := st.CreateRegistrationCode(ctx, userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateRegistrationCode: %v", err)
	}
	reg, err := RegisterClient(ctx, baseURL, code, "laptop")
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	if reg.Token == "" || reg.ClientName != "laptop" {
		t.Fatalf("register result incomplete: %+v", reg)
	}
	// 注册码一次性：重放被拒（统一 400 通用消息）
	if _, err := RegisterClient(ctx, baseURL, code, "other"); err == nil || !strings.Contains(err.Error(), "注册码无效") {
		t.Fatalf("replayed registration code should fail with generic message, got %v", err)
	}

	// 新机器 B：bootstrap 接入已有 vault
	cfgB, dataB := t.TempDir(), t.TempDir()
	pB := NewServerProvider(baseURL, reg.Token, cfgB, dataB, "main")
	if err := pB.Bootstrap(ctx); err != nil {
		t.Fatalf("machine B bootstrap: %v", err)
	}

	// B 建立解锁缓存
	mgrB := session.NewManager(cfgB, dataB)
	defer mgrB.Close()
	if err := mgrB.StartSession(password, &session.SessionTimeout{Type: session.TimeoutRestart}); err != nil {
		t.Fatalf("machine B start session: %v", err)
	}
	if cache, _ := mgrB.LoadCache(); cache == nil {
		t.Fatal("session cache should exist before block")
	}

	// 管理员屏蔽 laptop：机器 B 任一请求感知后清理解锁缓存
	if err := st.SetClientStatus(ctx, -1, "laptop", store.ClientStatusBlocked); err != nil {
		t.Fatalf("block client: %v", err)
	}
	if err := pB.Pull(); !errors.Is(err, ErrClientBlocked) {
		t.Fatalf("blocked pull err = %v, want ErrClientBlocked", err)
	}
	if cache, _ := mgrB.LoadCache(); cache != nil {
		t.Error("session cache should be cleared after blocked response")
	}
	// 本地加密数据保留：metadata 仍可读取
	smB := storage.NewManager(cfgB, dataB)
	if _, err := smB.LoadMetadata(); err != nil {
		t.Errorf("local encrypted data must be preserved: %v", err)
	}
	// 审计留痕
	home := os.Getenv("HOME")
	auditData, err := os.ReadFile(filepath.Join(home, ".log", "senv", "audit.log"))
	if err != nil {
		t.Fatalf("audit log missing: %v", err)
	}
	if !strings.Contains(string(auditData), "client_blocked") {
		t.Errorf("audit log should record client_blocked, got %q", string(auditData))
	}

	// 机器 A 不受影响（屏蔽粒度 = 单台设备）
	if _, err := pA.SyncWithReport(ctx); err != nil {
		t.Errorf("machine A must be unaffected by blocking machine B: %v", err)
	}

	// 解封后 B 原凭证恢复可用
	if err := st.SetClientStatus(ctx, -1, "laptop", store.ClientStatusActive); err != nil {
		t.Fatalf("unblock client: %v", err)
	}
	if err := pB.Pull(); err != nil {
		t.Errorf("after unblock pull should succeed, got %v", err)
	}
}
