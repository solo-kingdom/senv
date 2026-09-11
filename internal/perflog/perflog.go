// Package perflog 记录 client 关键路径耗时（耗时日志）：单个阶段耗时超过
// 阈值时追加一行 JSON 到 ~/.log/senv/perf.log。与操作审计（audit.log）语义
// 分离——审计记"做了什么"，本包只记"花了多久"；任何明文值与密钥材料不得
// 作为属性传入。写入失败静默降级，绝不影响业务路径。
//
// 环境变量：SENV_PERF=off 整体关闭（零写放大）；
// SENV_PERF_THRESHOLD 调整阈值（毫秒整数，非法或 ≤0 回退默认 100ms）。
package perflog

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// 环境变量与默认值；测试可直接改下方包级变量后复位。
const (
	EnvOff           = "SENV_PERF"
	EnvThreshold     = "SENV_PERF_THRESHOLD"
	OffValue         = "off"
	DefaultThreshold = 100 * time.Millisecond
)

var (
	initOnce  = &sync.Once{}
	logger    *slog.Logger // nil 表示关闭或初始化失败（no-op）
	threshold = DefaultThreshold
)

// Timer 是一个阶段计时器。Start 返回的指针总是非 nil，方法可安全用于 defer。
type Timer struct {
	stage string
	start time.Time
	attrs []any
}

// Start 开始计时一个阶段。stage 是阶段标识（如 tui.load-env、net.request），
// 不是用户数据。
func Start(stage string) *Timer {
	return &Timer{stage: stage, start: time.Now()}
}

// With 附加规模维度等属性（组数、条目数、建连数等）。键必须是非敏感标识。
func (t *Timer) With(args ...any) *Timer {
	t.attrs = append(t.attrs, args...)
	return t
}

// End 结束计时：耗时达到阈值才落盘一条 JSON 行；ok=false 记为失败。
func (t *Timer) End(ok bool) {
	ensureInit()
	if logger == nil {
		return
	}
	d := time.Since(t.start)
	if d < threshold {
		return
	}
	logger.Info(t.stage,
		append([]any{"duration_ms", d.Milliseconds(), "ok", ok}, t.attrs...)...)
}

// EndErr 按 err 是否为 nil 结束计时。
func (t *Timer) EndErr(err error) {
	t.End(err == nil)
}

// Note 记录一条不受阈值判断约束的即时事件（如建连次数汇总）。
func Note(stage string, args ...any) {
	ensureInit()
	if logger == nil {
		return
	}
	logger.Info(stage, args...)
}

// Enabled 报告耗时日志当前是否启用。
func Enabled() bool {
	ensureInit()
	return logger != nil
}

// ensureInit 保证生产初始化只执行一次；initOnce 用指针承载，测试可整体换新。
func ensureInit() {
	initOnce.Do(initFromEnv)
}

func initFromEnv() {
	if os.Getenv(EnvOff) == OffValue {
		return
	}
	threshold = parseThreshold(os.Getenv(EnvThreshold))
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return
	}
	dir := filepath.Join(home, ".log", "senv")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "perf.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	logger = slog.New(slog.NewJSONHandler(f, nil))
}

func parseThreshold(v string) time.Duration {
	if v == "" {
		return DefaultThreshold
	}
	ms, err := strconv.Atoi(v)
	if err != nil || ms <= 0 {
		return DefaultThreshold
	}
	return time.Duration(ms) * time.Millisecond
}
