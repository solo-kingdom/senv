// 访问日志中间件：每个非 healthz 请求在响应后落一条安全事件（best-effort）。
// 结果由响应状态码判定；认证失败原因由认证中间件写入 request context。
package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/wii/senv/internal/server/store"
)

// accessRecorder 记录响应状态码（WriteHeader 可能不被显式调用 → 200）
type accessRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *accessRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *accessRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}

// outcomeFromStatus 把响应状态映射为安全事件结果
func outcomeFromStatus(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return store.AccessOutcomeAuthFailed
	case http.StatusForbidden:
		return store.AccessOutcomeBlocked
	case http.StatusTooManyRequests:
		return store.AccessOutcomeRateLimited
	default:
		return store.AccessOutcomeOK
	}
}

// recordAccess 落一条安全事件；写入失败仅记服务端错误日志，不影响响应
func (s *Server) recordAccess(ctx context.Context, info *accessInfo, r *http.Request, rec *accessRecorder) {
	err := s.store.RecordAccess(ctx, store.AccessEvent{
		IP:       remoteIP(r),
		Method:   r.Method,
		Path:     r.URL.Path,
		ClientID: info.clientID,
		UserID:   info.userID,
		Outcome:  outcomeFromStatus(rec.status),
		Reason:   info.reason,
	})
	if err != nil {
		slog.Error("access log write failed", "err", err)
	}
}

// withAccessLog 包装 ServeHTTP：healthz 跳过（健康探测会淹没事件流水）。
// 每个请求挂一个可变 accessInfo，认证中间件原地写入身份与拦截原因。
func (s *Server) withAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		info := &accessInfo{}
		rec := &accessRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(contextWithAccessInfo(r.Context(), info)))
		s.recordAccess(r.Context(), info, r, rec)
	})
}
