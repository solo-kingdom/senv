// 限速器惰性清扫：过期条目在下次失败计数时被回收，map 不随历史失败 IP 无界增长。
package handler

import (
	"fmt"
	"testing"
	"time"
)

func TestRateLimiterSweepExpired(t *testing.T) {
	l := newAuthRateLimiter(5)

	// 50 个不同来源各失败一次
	for i := 0; i < 50; i++ {
		l.countFailure(fmt.Sprintf("10.0.0.%d", i))
	}
	l.mu.Lock()
	if len(l.failures) != 50 {
		l.mu.Unlock()
		t.Fatalf("entries before sweep = %d, want 50", len(l.failures))
	}
	// 手动把所有窗口置为已过期（真实场景由窗口时间流逝产生），
	// 并重置 lastSweep 模拟清扫窗口已过（首个 countFailure 已触发过一轮）
	for _, w := range l.failures {
		w.resetAt = time.Now().Add(-time.Second)
	}
	l.lastSweep = time.Time{}
	l.mu.Unlock()

	// 一次新失败触发惰性清扫：全部过期条目被删除，只留新条目
	l.countFailure("10.9.9.9")

	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.failures) != 1 {
		t.Errorf("entries after sweep = %d, want 1", len(l.failures))
	}
	if _, ok := l.failures["10.9.9.9"]; !ok {
		t.Error("new failure entry missing after sweep")
	}
}

func TestRateLimiterSweepKeepsActiveWindows(t *testing.T) {
	l := newAuthRateLimiter(5)

	l.countFailure("10.0.0.1") // 活跃窗口（未过期）
	l.countFailure("10.0.0.2")
	// 只把一个条目置为过期，并模拟清扫窗口已过
	l.mu.Lock()
	l.failures["10.0.0.2"].resetAt = time.Now().Add(-time.Second)
	l.lastSweep = time.Time{}
	l.mu.Unlock()

	l.countFailure("10.0.0.3")

	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.failures) != 2 {
		t.Errorf("entries = %d, want 2 (active + new; expired removed)", len(l.failures))
	}
	if _, ok := l.failures["10.0.0.1"]; !ok {
		t.Error("active window was swept")
	}
}
