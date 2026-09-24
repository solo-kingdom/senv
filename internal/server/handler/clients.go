// client 注册端点：凭一次性注册码换取 client 专属 Bearer token。
package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode"

	"github.com/wii/senv/internal/server/store"
)

// MaxClientNameLen 是设备名长度上限（与条目 grp 同量级，防御性约束）
const MaxClientNameLen = 128

type registerRequest struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type registerResponse struct {
	Token  string       `json:"token"`
	Client store.Client `json:"client"`
}

// handleRegister 处理 POST /v1/register。免认证但与认证失败共用同一限速器：
// 无效注册码计入失败，防止对注册码的在线枚举。无效/过期/已用注册码统一
// 400 通用消息，不区分具体原因（防枚举）；同名冲突 409 提示改名。
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	ip := s.resolveRemoteIP(r)
	if s.limiter.blocked(ip) {
		writeError(w, http.StatusTooManyRequests, "too many requests")
		return
	}
	var req registerRequest
	if err := s.decodeBody(w, r, &req); err != nil {
		s.writeDecodeError(w, err)
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	req.Name = strings.TrimSpace(req.Name)
	if req.Code == "" || req.Name == "" || len(req.Name) > MaxClientNameLen {
		writeError(w, http.StatusBadRequest, "code 与 name（1-128 字符）必填")
		return
	}
	// 拒绝控制字符：设备名会回显在 admin list-clients/logs 输出里，
	// 终端转义序列可污染管理员控制台
	for _, r := range req.Name {
		if unicode.IsControl(r) {
			writeError(w, http.StatusBadRequest, "name 不能包含控制字符")
			return
		}
	}

	token, client, err := s.store.RegisterClient(r.Context(), req.Code, req.Name)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			s.limiter.countFailure(ip)
			writeError(w, http.StatusBadRequest, "注册码无效或已过期")
		case errors.Is(err, store.ErrNameConflict):
			writeError(w, http.StatusConflict, "该用户下已存在同名 client，请更换设备名")
		default:
			slog.Error("register client failed", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	// 身份写回 accessInfo：访问事件与告警因此带 client/user 归属
	if info := accessInfoFrom(r.Context()); info != nil {
		info.userID = client.UserID
		info.clientID = client.ID
	}
	// 明文 token 只在注册响应中出现一次，库中仅存哈希
	writeJSON(w, http.StatusCreated, registerResponse{Token: token, Client: *client})
}
