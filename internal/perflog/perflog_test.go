package perflog

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// firedOnce 返回一个已触发的 Once，用于把生产初始化标记为已消费。
func firedOnce() *sync.Once {
	o := &sync.Once{}
	o.Do(func() {})
	return o
}

// withSink 把包级 sink 指到临时文件并复位阈值；测试结束恢复原状。
func withSink(t *testing.T, th time.Duration) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "perf.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open temp sink: %v", err)
	}
	oldLogger, oldThreshold, oldOnce := logger, threshold, initOnce
	logger = slog.New(slog.NewJSONHandler(f, nil))
	threshold = th
	initOnce = firedOnce() // 标记生产初始化已消费，避免覆盖注入的 sink
	t.Cleanup(func() {
		f.Close()
		logger, threshold, initOnce = oldLogger, oldThreshold, oldOnce
	})
	return path
}

func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read sink: %v", err)
	}
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("parse sink line %q: %v", line, err)
		}
		rows = append(rows, row)
	}
	return rows
}

func TestThresholdFilter(t *testing.T) {
	path := withSink(t, time.Hour)
	Start("below.threshold").End(true)
	if rows := readLines(t, path); len(rows) != 0 {
		t.Fatalf("低于阈值不应落盘，得到 %v", rows)
	}

	path = withSink(t, 0)
	Start("above.threshold").With("items", 3).End(true)
	rows := readLines(t, path)
	if len(rows) != 1 {
		t.Fatalf("达到阈值应恰好一行，得到 %d", len(rows))
	}
	if rows[0]["msg"] != "above.threshold" || rows[0]["ok"] != true || rows[0]["items"] != float64(3) {
		t.Fatalf("字段不符: %v", rows[0])
	}
	if _, ok := rows[0]["duration_ms"]; !ok {
		t.Fatalf("缺少 duration_ms: %v", rows[0])
	}
}

func TestEndErr(t *testing.T) {
	path := withSink(t, 0)
	Start("op.fail").EndErr(errBoom{})
	Start("op.ok").EndErr(nil)
	rows := readLines(t, path)
	if len(rows) != 2 || rows[0]["ok"] != false || rows[1]["ok"] != true {
		t.Fatalf("EndErr 应按 err 是否为 nil 记 ok: %v", rows)
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }

func TestDisabledNoWrite(t *testing.T) {
	oldLogger, oldOnce := logger, initOnce
	logger = nil           // 模拟 SENV_PERF=off / 初始化失败
	initOnce.Do(func() {}) // 标记初始化已消费，避免 Enabled 触发真实初始化
	t.Cleanup(func() { logger, initOnce = oldLogger, oldOnce })

	if Enabled() {
		t.Fatal("关闭时 Enabled 应为 false")
	}
	Start("noop.stage").End(true)
	Note("noop.note", "k", 1) // 不应 panic，也不产生任何输出
}

func TestNoteIgnoresThreshold(t *testing.T) {
	path := withSink(t, time.Hour)
	Note("conns.summary", "conns_new", 2)
	rows := readLines(t, path)
	if len(rows) != 1 || rows[0]["conns_new"] != float64(2) {
		t.Fatalf("Note 不受阈值约束: %v", rows)
	}
}

func TestParseThreshold(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", DefaultThreshold},
		{"250", 250 * time.Millisecond},
		{"abc", DefaultThreshold},
		{"-5", DefaultThreshold},
		{"0", DefaultThreshold},
	}
	for _, c := range cases {
		if got := parseThreshold(c.in); got != c.want {
			t.Errorf("parseThreshold(%q)=%v, want %v", c.in, got, c.want)
		}
	}
}

func TestInitFromEnvOff(t *testing.T) {
	oldLogger, oldOnce := logger, initOnce
	t.Cleanup(func() { logger, initOnce = oldLogger, oldOnce })
	logger, initOnce = nil, &sync.Once{}

	t.Setenv(EnvOff, OffValue)
	initFromEnv()
	if logger != nil {
		t.Fatal("SENV_PERF=off 时不应构造 sink")
	}
}
