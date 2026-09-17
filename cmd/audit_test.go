package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/session"
)

// newAuditTestProject 初始化隔离的测试 vault，把 configPathFn 与 dataPath
// 包级变量都指向测试目录（newInitializedProject 的返回值是局部变量，不会
// 改变全局路径；漏掉 dataPath 会让命令读到真实用户数据目录）。
func newAuditTestProject(t *testing.T) (cfg, data string) {
	t.Helper()
	isolateSessionCache(t)
	dir := t.TempDir()
	cfg, data = newInitializedProject(t, dir, "audit-password")
	prevCfg := configPathFn
	configPathFn = func() string { return cfg }
	t.Cleanup(func() { configPathFn = prevCfg })
	prevData := dataPath
	dataPath = data
	t.Cleanup(func() { dataPath = prevData })
	authPrompt = stubPrompter("audit-password")
	return cfg, data
}

func readAuditLogForTest(t *testing.T) string {
	t.Helper()
	path := session.AuditLogPath()
	if path == "" {
		t.Fatal("AuditLogPath empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read audit log: %v", err)
	}
	return string(data)
}

func TestEnvSetDeleteWriteAuditEvents(t *testing.T) {
	newAuditTestProject(t)

	if err := envSetCmd.RunE(&cobra.Command{}, []string{"API_KEY", "v"}); err != nil {
		t.Fatalf("env set: %v", err)
	}
	if err := envDeleteCmd.RunE(&cobra.Command{}, []string{"API_KEY"}); err != nil {
		t.Fatalf("env delete: %v", err)
	}

	log := readAuditLogForTest(t)
	if !strings.Contains(log, `"event_type":"op_env"`) {
		t.Errorf("audit log should contain op_env events, got %q", log)
	}
	if !strings.Contains(log, `"target":"env:default:API_KEY"`) {
		t.Errorf("audit log should contain target env:default:API_KEY, got %q", log)
	}
	// 失败留痕
	if err := envDeleteCmd.RunE(&cobra.Command{}, []string{"GHOST_KEY"}); err == nil {
		t.Fatal("deleting nonexistent key should fail")
	}
	log = readAuditLogForTest(t)
	if !strings.Contains(log, `"success":false`) {
		t.Errorf("failed operation should be recorded with success=false, got %q", log)
	}
	// 不含值：设置值 "v" 不应出现在审计中
	if strings.Contains(log, "\":\"v\"") {
		t.Errorf("audit log must not contain the value itself: %q", log)
	}
}

func TestTextSetWritesAuditEvent(t *testing.T) {
	newAuditTestProject(t)
	textMgr, err := getTextManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := textMgr.AddGroup("notes", "test"); err != nil {
		t.Fatal(err)
	}

	if err := textSetCmd.RunE(&cobra.Command{}, []string{"notes:README", "hello"}); err != nil {
		t.Fatalf("text set: %v", err)
	}
	log := readAuditLogForTest(t)
	if !strings.Contains(log, `"target":"text:notes:README"`) {
		t.Errorf("audit log should contain text target, got %q", log)
	}
	if strings.Contains(log, "hello") {
		t.Errorf("audit log must not contain the text value, got %q", log)
	}
}

func TestAuditViewerFiltersAndSkipsBadLines(t *testing.T) {
	newAuditTestProject(t)

	if err := envSetCmd.RunE(&cobra.Command{}, []string{"K1", "v"}); err != nil {
		t.Fatalf("env set: %v", err)
	}
	// 失败事件（✗）
	if err := envDeleteCmd.RunE(&cobra.Command{}, []string{"GHOST_KEY"}); err == nil {
		t.Fatal("deleting nonexistent key should fail")
	}
	// 手工追加坏行模拟手工编辑损坏
	path := session.AuditLogPath()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open audit: %v", err)
	}
	f.WriteString("this-is-not-json\n")
	f.Close()

	entries, skipped, err := loadAuditEntries()
	if err != nil {
		t.Fatalf("loadAuditEntries: %v", err)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	// 新到旧：第一条是最近写入的 op_env
	if len(entries) == 0 || entries[0].EventType != session.AuditOpEnv {
		t.Fatalf("entries[0] = %+v, want op_env newest first", entries)
	}
	// 类型过滤
	if !auditTypeMatches("op", "op_env") || auditTypeMatches("op", "session_start") ||
		auditTypeMatches("op_env", "op_text") || !auditTypeMatches("", "anything") {
		t.Errorf("auditTypeMatches semantics broken")
	}

	// 查看器渲染（捕获 stdout）
	auditLimit = 10
	out := captureStdout(t, func() {
		if err := runAuditView(&cobra.Command{}, nil); err != nil {
			t.Fatalf("runAuditView: %v", err)
		}
	})
	if !strings.Contains(out, "op_env") || !strings.Contains(out, "✓") || !strings.Contains(out, "✗") {
		t.Errorf("audit view should list events with outcomes, got %q", out)
	}
	if !strings.Contains(out, "跳过 1 行") {
		t.Errorf("audit view should report skipped lines, got %q", out)
	}
}

func TestLogOpBestEffort(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	al, err := session.NewAuditLogger(t.TempDir())
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	// 正常写入
	if err := al.LogOp(session.AuditOpSync, "vault:main", true, "test"); err != nil {
		t.Fatalf("LogOp: %v", err)
	}
	// 关闭底层文件后写入 → 返回错误但不 panic（调用方忽略错误，业务不受影响）
	if err := al.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := al.LogOp(session.AuditOpSync, "vault:main", true, "second"); err == nil {
		t.Log("write after Close unexpectedly succeeded")
	}
}

func TestAuditHatchCacheSelectedOnce(t *testing.T) {
	newAuditTestProject(t)

	prevProbe := hatchCacheSelectedProbe
	hatchCacheSelectedProbe = func() bool { return true }
	t.Cleanup(func() { hatchCacheSelectedProbe = prevProbe })
	auditHatchLogged.Store(false)
	t.Cleanup(func() { auditHatchLogged.Store(false) })

	auditHatchCacheSelectedOnce()
	log := readAuditLogForTest(t)
	if !strings.Contains(log, `"message":"cache-source=disk-hatch"`) ||
		!strings.Contains(log, `"target":"session:cache"`) {
		t.Fatalf("audit log missing hatch trace: %q", log)
	}

	// 进程内第二次不重复记录。
	before := strings.Count(log, "cache-source=disk-hatch")
	auditHatchCacheSelectedOnce()
	if after := strings.Count(readAuditLogForTest(t), "cache-source=disk-hatch"); after != before {
		t.Fatalf("hatch audit recorded %d times, want once", after)
	}

	// probe 为 false（未选逃生舱）时不记录。
	auditHatchLogged.Store(false)
	hatchCacheSelectedProbe = func() bool { return false }
	auditHatchCacheSelectedOnce()
	if after := strings.Count(readAuditLogForTest(t), "cache-source=disk-hatch"); after != before {
		t.Fatalf("hatch audit recorded without selection: %d entries", after)
	}
}
