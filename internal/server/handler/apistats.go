package handler

import (
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wii/senv/internal/server/store"
)

//go:embed statsui/index.html
var statsUIFS embed.FS

func (s *Server) statsProtected(h http.HandlerFunc) http.HandlerFunc {
	return s.lanOnly(s.rejectSyncToken(h))
}

func (s *Server) handleStatsPage(w http.ResponseWriter, r *http.Request) {
	data, err := statsUIFS.ReadFile("statsui/index.html")
	if err != nil {
		slog.Error("read embedded stats page failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func parseAPIStatsQuery(r *http.Request) (store.APIStatsQuery, error) {
	qv := r.URL.Query()
	rng := strings.TrimSpace(qv.Get("range"))
	if rng == "" {
		rng = "24h"
	}
	switch rng {
	case "1h", "24h", "7d", "30d":
	default:
		return store.APIStatsQuery{}, fmt.Errorf("invalid range")
	}

	sortBy := strings.TrimSpace(qv.Get("sort"))
	if sortBy == "" {
		sortBy = "count"
	}
	switch sortBy {
	case "count", "error_rate", "p95":
	default:
		return store.APIStatsQuery{}, fmt.Errorf("invalid sort")
	}

	statusClass := strings.TrimSpace(qv.Get("status_class"))
	if statusClass != "" {
		switch strings.ToLower(statusClass) {
		case "2xx", "3xx", "4xx", "5xx":
		default:
			return store.APIStatsQuery{}, fmt.Errorf("invalid status_class")
		}
	}

	q := store.APIStatsQuery{
		Range:       rng,
		Method:      strings.TrimSpace(qv.Get("method")),
		Route:       strings.TrimSpace(qv.Get("route")),
		StatusClass: strings.ToLower(statusClass),
		AuthType:    strings.TrimSpace(qv.Get("auth_type")),
		ApiKeyID:    strings.TrimSpace(qv.Get("api_key_id")),
		Sort:        sortBy,
	}

	if rawLimit := strings.TrimSpace(qv.Get("limit")); rawLimit != "" {
		n, err := strconv.Atoi(rawLimit)
		if err != nil || n <= 0 {
			return store.APIStatsQuery{}, fmt.Errorf("invalid limit")
		}
		q.Limit = n
	}
	if rawFrom := strings.TrimSpace(qv.Get("from")); rawFrom != "" {
		t, err := time.Parse(time.RFC3339, rawFrom)
		if err != nil {
			return store.APIStatsQuery{}, fmt.Errorf("invalid from")
		}
		q.From = t.UTC()
	}
	if rawTo := strings.TrimSpace(qv.Get("to")); rawTo != "" {
		t, err := time.Parse(time.RFC3339, rawTo)
		if err != nil {
			return store.APIStatsQuery{}, fmt.Errorf("invalid to")
		}
		q.To = t.UTC()
	}
	return q, nil
}

func writeStatsJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleStatsOverview(w http.ResponseWriter, r *http.Request) {
	q, err := parseAPIStatsQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.store.StatsOverview(r.Context(), q)
	if err != nil {
		slog.Error("stats overview failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeStatsJSON(w, res)
}

func (s *Server) handleStatsRoutes(w http.ResponseWriter, r *http.Request) {
	q, err := parseAPIStatsQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.store.StatsRoutes(r.Context(), q)
	if err != nil {
		slog.Error("stats routes failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeStatsJSON(w, res)
}

func (s *Server) handleStatsErrors(w http.ResponseWriter, r *http.Request) {
	q, err := parseAPIStatsQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.store.StatsErrors(r.Context(), q)
	if err != nil {
		slog.Error("stats errors failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeStatsJSON(w, res)
}

func (s *Server) handleStatsAPIKeys(w http.ResponseWriter, r *http.Request) {
	q, err := parseAPIStatsQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.store.StatsAPIKeys(r.Context(), q)
	if err != nil {
		slog.Error("stats api keys failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeStatsJSON(w, res)
}

func (s *Server) handleStatsAPIKeyDetail(w http.ResponseWriter, r *http.Request) {
	q, err := parseAPIStatsQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	keyID := strings.TrimSpace(r.PathValue("keyId"))
	if keyID == "" {
		writeError(w, http.StatusBadRequest, "keyId is required")
		return
	}
	res, err := s.store.StatsAPIKeyDetail(r.Context(), keyID, q)
	if err != nil {
		slog.Error("stats api key detail failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeStatsJSON(w, res)
}
