package handler

import (
	"net"
	"net/http"
	"strings"
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
	mu        sync.Mutex
	limit     int // 窗口内允许的认证失败次数
	window    time.Duration
	failures  map[string]*authWindow
	lastSweep time.Time
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
	l.sweepExpired(now)
	w, ok := l.failures[ip]
	if !ok || now.After(w.resetAt) {
		l.failures[ip] = &authWindow{count: 1, resetAt: now.Add(l.window)}
		return
	}
	w.count++
}

// sweepExpired 删除已过窗口的条目，防止分布式扫描以大量唯一失败 IP 无限
// 撑大 failures map。惰性触发：每个窗口至多一次全表遍历（调用方持锁）。
func (l *authRateLimiter) sweepExpired(now time.Time) {
	if now.Sub(l.lastSweep) < l.window {
		return
	}
	for ip, w := range l.failures {
		if now.After(w.resetAt) {
			delete(l.failures, ip)
		}
	}
	l.lastSweep = now
}

// remoteIP 取请求来源 IP（去掉端口）。解析失败时退回原始 RemoteAddr。
func remoteIP(r *http.Request) string {
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}

// resolveRemoteIP 解析请求来源的客户端 IP，是限速与访问日志共用的唯一取值点。
//
// 仅当 TrustProxyHeaders 开启且直连对端是 loopback 或私网地址（同机反代，
// 或 docker 网桥/内网反代拓扑）时，才采信代理头；其余情况一律使用连接对端。
// 优先级：
//  1. X-Real-IP（须为合法 IP；需由反代强制覆写，避免透传客户端伪造值）
//  2. X-Forwarded-For 自右向左跳过可信代理（loopback/私网）取最右公网 IP；
//     若链路全为合法私网/回环地址（纯内网反代拓扑），取最右合法 IP（直连反代的
//     真实对端）；最右项本身非法则 fail-safe 回落连接对端，防止攻击者在最左段
//     伪造任意 IP 绕过限速或污染审计日志。
func (s *Server) resolveRemoteIP(r *http.Request) string {
	host := remoteIP(r)
	if !s.trustProxyHeaders {
		return host
	}
	peer := net.ParseIP(host)
	if peer == nil || !(peer.IsLoopback() || peer.IsPrivate()) {
		return host
	}
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
		if ip := net.ParseIP(v); ip != nil {
			return ip.String()
		}
	}
	if ip := extractForwardedClientIP(r.Header.Values("X-Forwarded-For")); ip != "" {
		return ip
	}
	return host
}

// extractForwardedClientIP 从所有 X-Forwarded-For 头中自右向左解析真实客户端 IP。
// 反代总是把收到的对端追加到最右端，因此最左端由不可信客户端控制，绝不能直接采信。
func extractForwardedClientIP(values []string) string {
	if len(values) == 0 {
		return ""
	}
	var parts []string
	for _, raw := range values {
		for _, seg := range strings.Split(raw, ",") {
			if seg = strings.TrimSpace(seg); seg != "" {
				parts = append(parts, seg)
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	rightmostValid := ""
	for i := len(parts) - 1; i >= 0; i-- {
		ip := net.ParseIP(parts[i])
		if ip == nil {
			// 最右端本身非法说明代理链未附合法来源，直接放弃；若此前已经
			// 穿过若干合法私网代理跳，停止解析并返回最右合法私网对端。
			return rightmostValid
		}
		if rightmostValid == "" {
			rightmostValid = ip.String()
		}
		if !(ip.IsLoopback() || ip.IsPrivate()) {
			return ip.String()
		}
	}
	return rightmostValid
}
