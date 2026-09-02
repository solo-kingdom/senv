// Package handler 负责 senv-server 的 HTTP 层：路由、Bearer 认证中间件与 JSON 编解码。
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/wii/senv/internal/server/store"
)

// maxBodySize 请求体上限：覆盖单批 1000 条 × 512KB 的理论最大值再加余量
const maxBodySize = 600 << 20

// contextKey 是 request context 中用户 ID 的键
type contextKey string

const userIDKey contextKey = "userID"

// Server 聚合依赖，实现 http.Handler
type Server struct {
	store *store.Store
	mux   *http.ServeMux
}

// New 创建 HTTP server（路由带 v1 前缀；健康检查除外，均需 Bearer token）
func New(st *store.Store) *Server {
	s := &Server{store: st, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /v1/vaults/{vault}/metadata", s.auth(s.handleGetMetadata))
	s.mux.HandleFunc("PUT /v1/vaults/{vault}/metadata", s.auth(s.handlePutMetadata))
	s.mux.HandleFunc("GET /v1/vaults/{vault}/entries", s.auth(s.handlePull))
	s.mux.HandleFunc("POST /v1/vaults/{vault}/entries", s.auth(s.handlePush))
	return s
}

// ServeHTTP 实现 http.Handler
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// auth 是 Bearer 认证中间件：无效/缺失/已吊销一律 401，不泄露 token 存在性
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		userID, err := s.store.Authenticate(r.Context(), token)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			slog.Error("authenticate failed", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		ctx := contextWithUserID(r.Context(), userID)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// metadataRequest/Response 的 blob 为客户端产物的不透明数据（base64 由 encoding/json 处理）
type metadataRequest struct {
	Blob []byte `json:"blob"`
}

func (s *Server) handleGetMetadata(w http.ResponseWriter, r *http.Request) {
	userID := userIDFrom(r.Context())
	vault := r.PathValue("vault")
	blob, err := s.store.GetMetadata(r.Context(), userID, vault)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "vault metadata not found")
			return
		}
		slog.Error("get metadata failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, metadataRequest{Blob: blob})
}

func (s *Server) handlePutMetadata(w http.ResponseWriter, r *http.Request) {
	userID := userIDFrom(r.Context())
	vault := r.PathValue("vault")
	var req metadataRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.PutMetadata(r.Context(), userID, vault, req.Blob); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// pushRequest 乐观锁批量推送请求
type pushRequest struct {
	Entries []store.Entry `json:"entries"`
}

// pushResponse 无冲突推送的响应：每条的新 revision 与最新 revision
type pushResponse struct {
	Revisions      []revisionAck `json:"revisions"`
	LatestRevision int64         `json:"latest_revision"`
}

type revisionAck struct {
	Kind     string `json:"kind"`
	Grp      string `json:"grp"`
	Key      string `json:"key"`
	Revision int64  `json:"revision"`
}

func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	userID := userIDFrom(r.Context())
	vault := r.PathValue("vault")
	var req pushRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, latest, err := s.store.PushEntries(r.Context(), userID, vault, req.Entries)
	if err != nil {
		var conflictErr *store.ConflictError
		if errors.As(err, &conflictErr) {
			// 409 响应必须列出冲突条目与 server 端当前 revision
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":     "revision conflict",
				"conflicts": conflictErr.Conflicts,
			})
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	acks := make([]revisionAck, 0, len(entries))
	for _, e := range entries {
		acks = append(acks, revisionAck{Kind: e.Kind, Grp: e.Grp, Key: e.Key, Revision: e.Revision})
	}
	writeJSON(w, http.StatusOK, pushResponse{Revisions: acks, LatestRevision: latest})
}

// pullResponse 增量拉取响应：revision > since 的全部条目（含删除标记）与最新 revision
type pullResponse struct {
	Entries        []store.Entry `json:"entries"`
	LatestRevision int64         `json:"latest_revision"`
}

func (s *Server) handlePull(w http.ResponseWriter, r *http.Request) {
	userID := userIDFrom(r.Context())
	vault := r.PathValue("vault")
	since, err := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	if err != nil || since < 0 {
		writeError(w, http.StatusBadRequest, "invalid since parameter")
		return
	}
	entries, latest, err := s.store.PullEntries(r.Context(), userID, vault, since)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "vault not found")
			return
		}
		slog.Error("pull failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, pullResponse{Entries: entries, LatestRevision: latest})
}

// decodeBody 限制请求体大小并解码 JSON
func decodeBody(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid JSON body: " + err.Error())
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
