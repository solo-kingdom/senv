package provider

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/server/handler"
	"github.com/wii/senv/internal/server/store"
	"github.com/wii/senv/internal/server/testdb"
	"github.com/wii/senv/internal/storage"
)

// e2eEnv 搭建真实 server（Postgres + HTTP）与一个已创建的用户 token。
// 返回的 down 开关用于模拟断网。
func e2eEnv(t *testing.T) (baseURL string, token string, down *atomic.Bool) {
	t.Helper()
	pool := testdb.New(t)
	st := store.New(pool)
	tok, err := st.CreateUser(context.Background(), "e2e-user")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	down = &atomic.Bool{}
	inner := handler.New(st)
	srv := httptest.NewServer(nil)
	// 模拟断网开关：down 时直接断开连接
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, err := hj.Hijack()
				if err == nil {
					conn.Close()
					return
				}
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		inner.ServeHTTP(w, r)
	})
	t.Cleanup(srv.Close)
	return srv.URL, tok, down
}

// deriveKey 与 session 机制同源：PBKDF2 派生 + passwordKey 校验
func deriveKey(t *testing.T, sm *storage.Manager, password string) []byte {
	t.Helper()
	ok, err := sm.VerifyPassword(password)
	if err != nil || !ok {
		t.Fatalf("VerifyPassword: ok=%v err=%v", ok, err)
	}
	md, err := sm.LoadMetadata()
	if err != nil {
		t.Fatalf("LoadMetadata: %v", err)
	}
	salt, err := base64.StdEncoding.DecodeString(md.Salt)
	if err != nil {
		t.Fatalf("decode salt: %v", err)
	}
	iterations, err := md.ValidatedKDFIterations()
	if err != nil {
		t.Fatalf("validate KDF iterations: %v", err)
	}
	return crypto.DeriveKeyWithIterations(password, salt, iterations)
}

// newLocalVault 在临时目录初始化一个本地 vault 并写入一个 env 变量
func newLocalVault(t *testing.T, password string) (configPath, dataPath string, key []byte) {
	t.Helper()
	configPath = t.TempDir()
	dataPath = t.TempDir()
	sm := storage.NewManager(configPath, dataPath)
	if err := sm.Initialize(password); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	key = deriveKey(t, sm, password)
	entry := &storage.EnvVarEntry{Value: "secret-value", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := sm.SaveEnvVarWithKey("default", "API_KEY", entry, key); err != nil {
		t.Fatalf("SaveEnvVarWithKey: %v", err)
	}
	return configPath, dataPath, key
}

func writeEnvVarE2E(t *testing.T, configPath, dataPath string, key []byte, name, value string) {
	t.Helper()
	sm := storage.NewManager(configPath, dataPath)
	entry := &storage.EnvVarEntry{Value: value, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := sm.SaveEnvVarWithKey("default", name, entry, key); err != nil {
		t.Fatalf("save env var: %v", err)
	}
}

// TestE2EServerProvider 端到端：机器 A 推送本地 vault → 机器 B bootstrap 接入 → 双向同步收敛
func TestE2EServerProvider(t *testing.T) {
	baseURL, token, _ := e2eEnv(t)
	ctx := context.Background()
	password := "e2e-password"

	// 机器 A：已有本地 vault，配置 server provider 后首次同步（server 端自动建 vault）
	cfgA, dataA, keyA := newLocalVault(t, password)
	pA := NewServerProvider(baseURL, token, cfgA, dataA, "main")
	if _, err := pA.SyncWithReport(ctx); err != nil {
		t.Fatalf("machine A initial sync: %v", err)
	}

	// 机器 B：新机器 bootstrap 接入已有 vault
	cfgB, dataB := t.TempDir(), t.TempDir()
	pB := NewServerProvider(baseURL, token, cfgB, dataB, "main")
	if err := pB.Bootstrap(ctx); err != nil {
		t.Fatalf("machine B bootstrap: %v", err)
	}

	// B 的本地缓存：口令可解锁（session 机制复用的同一套校验）
	smB := storage.NewManager(cfgB, dataB)
	keyB := deriveKey(t, smB, password)
	// B 能解密 A 写入的 env 变量（缓存格式与本地存储一致）
	entry, err := smB.LoadEnvVarWithKey("default", "API_KEY", keyB)
	if err != nil {
		t.Fatalf("machine B LoadEnvVarWithKey: %v", err)
	}
	if entry.Value != "secret-value" {
		t.Errorf("machine B decrypted value = %q, want secret-value", entry.Value)
	}

	// B 写入新变量并同步 → A 拉取后可解密
	entryB := &storage.EnvVarEntry{Value: "from-b", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := smB.SaveEnvVarWithKey("prod", "TOKEN", entryB, keyB); err != nil {
		t.Fatalf("machine B save: %v", err)
	}
	if _, err := pB.SyncWithReport(ctx); err != nil {
		t.Fatalf("machine B sync: %v", err)
	}
	if _, err := pA.SyncWithReport(ctx); err != nil {
		t.Fatalf("machine A sync: %v", err)
	}
	smA := storage.NewManager(cfgA, dataA)
	got, err := smA.LoadEnvVarWithKey("prod", "TOKEN", keyA)
	if err != nil {
		t.Fatalf("machine A load after pull: %v", err)
	}
	if got.Value != "from-b" {
		t.Errorf("machine A value = %q, want from-b", got.Value)
	}
}

// TestE2EConflictAndOffline 端到端：409 冲突清单解析 + 断网报错与恢复收敛
func TestE2EConflictAndOffline(t *testing.T) {
	baseURL, token, down := e2eEnv(t)
	ctx := context.Background()
	password := "e2e-password-2"

	cfgA, dataA, keyA := newLocalVault(t, password)
	pA := NewServerProvider(baseURL, token, cfgA, dataA, "main")
	if _, err := pA.SyncWithReport(ctx); err != nil {
		t.Fatalf("A initial sync: %v", err)
	}

	cfgB, dataB := t.TempDir(), t.TempDir()
	pB := NewServerProvider(baseURL, token, cfgB, dataB, "main")
	if err := pB.Bootstrap(ctx); err != nil {
		t.Fatalf("B bootstrap: %v", err)
	}

	// A 与 B 同时修改同一条目 → B 同步时收到冲突清单，两端数据不变
	writeEnvVarE2E(t, cfgA, dataA, keyA, "API_KEY", "a-value")
	if _, err := pA.SyncWithReport(ctx); err != nil {
		t.Fatalf("A sync: %v", err)
	}
	smB := storage.NewManager(cfgB, dataB)
	keyB := deriveKey(t, smB, password)
	writeEnvVarE2E(t, cfgB, dataB, keyB, "API_KEY", "b-value")
	_, err := pB.SyncWithReport(ctx)
	var conflictErr *SyncConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("B sync error = %v, want SyncConflictError", err)
	}
	if len(conflictErr.Conflicts) != 1 || conflictErr.Conflicts[0].Key != "API_KEY" {
		t.Errorf("conflicts = %+v, want API_KEY", conflictErr.Conflicts)
	}
	entry, _ := smB.LoadEnvVarWithKey("default", "API_KEY", keyB)
	if entry.Value != "b-value" {
		t.Errorf("B local value changed to %q, want b-value", entry.Value)
	}

	// 断网：同步明确报错，本地数据不受影响
	down.Store(true)
	if _, err := pB.SyncWithReport(ctx); err == nil {
		t.Fatal("offline sync should fail")
	}
	if _, err := os.Stat(filepath.Join(dataB, "envs", "default", "API_KEY.enc")); err != nil {
		t.Errorf("local cache must survive offline failure: %v", err)
	}

	// 恢复：force-push 以本地为准收敛，之后正常同步无冲突
	down.Store(false)
	if err := pB.ForcePush(ctx); err != nil {
		t.Fatalf("force-push after recovery: %v", err)
	}
	if _, err := pB.SyncWithReport(ctx); err != nil {
		t.Fatalf("sync after recovery should converge: %v", err)
	}
	// A 拉取后看到 B 的值
	if _, err := pA.SyncWithReport(ctx); err != nil {
		t.Fatalf("A final sync: %v", err)
	}
	smA := storage.NewManager(cfgA, dataA)
	got, err := smA.LoadEnvVarWithKey("default", "API_KEY", keyA)
	if err != nil {
		t.Fatalf("A load after converge: %v", err)
	}
	if got.Value != "b-value" {
		t.Errorf("A value after converge = %q, want b-value", got.Value)
	}
}
