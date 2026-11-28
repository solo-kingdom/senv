package handler

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

var defaultLANPrefixes = mustParsePrefixes([]string{
	"127.0.0.0/8",
	"::1/128",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"100.64.0.0/10",
	"fc00::/7",
	"fe80::/10",
})

func mustParsePrefixes(raw []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(raw))
	for _, item := range raw {
		out = append(out, netip.MustParsePrefix(item))
	}
	return out
}

// LANChecker 校验请求是否来自内网（IP 链路 + Host 双重 fail-closed 门禁）。
type LANChecker struct {
	prefixes     []netip.Prefix
	allowedHosts map[string]struct{}
	invalid      bool
}

// NewLANChecker 构造内网校验器；任一自定义 CIDR 无法解析时标记 invalid（fail-closed）。
func NewLANChecker(allowedCIDRs, allowedHosts []string) *LANChecker {
	prefixes := make([]netip.Prefix, 0, len(defaultLANPrefixes)+len(allowedCIDRs))
	prefixes = append(prefixes, defaultLANPrefixes...)
	invalid := false
	for _, raw := range allowedCIDRs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		pfx, err := netip.ParsePrefix(trimmed)
		if err != nil {
			invalid = true
			continue
		}
		prefixes = append(prefixes, pfx)
	}
	hosts := make(map[string]struct{}, len(allowedHosts))
	for _, raw := range allowedHosts {
		if norm := normalizeHostName(raw); norm != "" {
			hosts[norm] = struct{}{}
		}
	}
	return &LANChecker{
		prefixes:     prefixes,
		allowedHosts: hosts,
		invalid:      invalid,
	}
}

// IsLANRequest 判断请求是否同时满足内网 IP 链路与内网 Host 规则。
func (l *LANChecker) IsLANRequest(req *http.Request) bool {
	if l == nil || l.invalid || req == nil {
		return false
	}
	if !l.checkIPChain(req) {
		return false
	}
	if !l.checkHosts(req) {
		return false
	}
	return true
}

func (l *LANChecker) checkIPChain(req *http.Request) bool {
	remoteIP, ok := parseAddrOrHostPort(req.RemoteAddr)
	if !ok || !l.ipAllowed(remoteIP) {
		return false
	}
	if xri := strings.TrimSpace(req.Header.Get("X-Real-IP")); xri != "" {
		ip, ok := parseAddrOrHostPort(xri)
		if !ok || !l.ipAllowed(ip) {
			return false
		}
	}
	for _, headerVal := range req.Header.Values("X-Forwarded-For") {
		if strings.TrimSpace(headerVal) == "" {
			return false
		}
		for _, part := range strings.Split(headerVal, ",") {
			ip, ok := parseAddrOrHostPort(part)
			if !ok || !l.ipAllowed(ip) {
				return false
			}
		}
	}
	return true
}

func (l *LANChecker) checkHosts(req *http.Request) bool {
	if !l.hostAllowed(req.Host) {
		return false
	}
	for _, headerVal := range req.Header.Values("X-Forwarded-Host") {
		if strings.TrimSpace(headerVal) == "" {
			return false
		}
		for _, part := range strings.Split(headerVal, ",") {
			if !l.hostAllowed(part) {
				return false
			}
		}
	}
	return true
}

func (l *LANChecker) ipAllowed(ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, pfx := range l.prefixes {
		if pfx.Contains(ip) {
			return true
		}
	}
	return false
}

func (l *LANChecker) hostAllowed(raw string) bool {
	h := normalizeHostName(raw)
	if h == "" {
		return false
	}
	if ip, err := netip.ParseAddr(h); err == nil {
		return l.ipAllowed(ip)
	}
	if _, ok := l.allowedHosts[h]; ok {
		return true
	}
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	if strings.HasSuffix(h, ".lan") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") {
		return true
	}
	if !strings.Contains(h, ".") && isSingleLabelHost(h) {
		return true
	}
	return false
}

func parseAddrOrHostPort(raw string) (netip.Addr, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return netip.Addr{}, false
	}
	if addr, err := netip.ParseAddr(strings.Trim(s, "[]")); err == nil {
		return addr.Unmap(), true
	}
	host, _, err := net.SplitHostPort(s)
	if err != nil {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func normalizeHostName(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "[") {
		if host, _, err := net.SplitHostPort(s); err == nil {
			s = host
		} else {
			s = strings.Trim(s, "[]")
		}
	} else if strings.Count(s, ":") == 1 {
		if host, _, err := net.SplitHostPort(s); err == nil {
			s = host
		}
	}
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}

func isSingleLabelHost(h string) bool {
	if h == "" || len(h) > 63 {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			continue
		}
		return false
	}
	return h[0] != '-' && h[len(h)-1] != '-'
}

// lanOnly 门禁：非内网来源或非内网 Host 一律 403。
func (s *Server) lanOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.lanChecker == nil || !s.lanChecker.IsLANRequest(r) {
			writeError(w, http.StatusForbidden, "forbidden: lan only")
			return
		}
		next(w, r)
	}
}

// rejectSyncToken 门禁：任何携带同步 Bearer Token / API Key 的请求访问统计页面或 API 一律 403。
func (s *Server) rejectSyncToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("Authorization")) != "" ||
			strings.TrimSpace(r.Header.Get("X-API-Key")) != "" {
			writeError(w, http.StatusForbidden, "api_key_forbidden")
			return
		}
		next(w, r)
	}
}
