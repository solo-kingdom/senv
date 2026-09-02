package handler

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// authRateLimiter 是认证失败的固定窗口限速器（单实例内存态，进程重启清零）。
//
// 只统计认证失败的请求；窗口内失败次数超过 limit 的来源在窗口剩余时间内
// 直接 429，不再触发数据库查询。按连接对端 IP（r.RemoteAddr 的 host 部分）
// 计数——若部署在反向代理之后，所有客户端共享代理的一个窗口，此时限速器
// 退化为总量保护，仍能阻止无限速的 token 爆破打到数据库。
type authRateLimiter struct {
	mu       sync.Mutex
	limit    int // 窗口内允许的认证失败次数
	window   time.Duration
	failures map[string]*authWindow
}

type authWindow struct {
	count   int
	resetAt time.Time
}

const defaultAuthRateLimit = 30

// newAuthRateLimiter 创建限速器。调用方保证 limit > 0（关闭时传 nil）。
func newAuthRateLimiter(limit int) *authRateLimiter {
	return &authRateLimiter{
		limit:    limit,
		window:   time.Minute,
		failures: make(map[string]*authWindow),
	}
}

// blocked 报告该来源当前是否已超过失败阈值。
func (l *authRateLimiter) blocked(ip string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.failures[ip]
	if !ok {
		return false
	}
	if time.Now().After(w.resetAt) {
		delete(l.failures, ip)
		return false
	}
	return w.count >= l.limit
}

// countFailure 在一次认证失败后调用。
func (l *authRateLimiter) countFailure(ip string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	w, ok := l.failures[ip]
	if !ok || now.After(w.resetAt) {
		l.failures[ip] = &authWindow{count: 1, resetAt: now.Add(l.window)}
		return
	}
	w.count++
}

// remoteIP 取请求来源 IP（去掉端口）。解析失败时退回原始 RemoteAddr。
func remoteIP(r *http.Request) string {
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}
