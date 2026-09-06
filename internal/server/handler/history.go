// 条目历史查询端点：只读、复用 Bearer 认证与 vault 隔离。
package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/wii/senv/internal/server/store"
)

// handleHistory 处理 GET /v1/vaults/{vault}/history。
// 查询参数 kind/grp/key 可组合过滤；指定 key 时按该条目 revision 新到旧返回，
// 否则返回 vault 级按时间新到旧的最近变更；limit 缺省 100、上限 1000。
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	userID := userIDFrom(r.Context())
	vault := r.PathValue("vault")
	f := store.HistoryFilter{
		Kind: r.URL.Query().Get("kind"),
		Grp:  r.URL.Query().Get("grp"),
		Key:  r.URL.Query().Get("key"),
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit parameter")
			return
		}
		f.Limit = limit
	}
	history, err := s.store.ListHistory(r.Context(), userID, vault, f)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "vault not found")
			return
		}
		slog.Error("history query failed", "vault", vault, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": history})
}
