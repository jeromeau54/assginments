package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-redis/redis/v8"
)

type Handlers struct {
	rdb         *redis.Client
	startTime   time.Time
	requestCount atomic.Int64
}

func New(rdb *redis.Client) *Handlers {
	return &Handlers{
		rdb:       rdb,
		startTime: time.Now(),
	}
}

func (h *Handlers) Root(w http.ResponseWriter, r *http.Request) {
	h.requestCount.Add(1)
	writeJSON(w, http.StatusOK, map[string]string{
		"service": "gateway",
		"status":  "ok",
	})
}

func (h *Handlers) Healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "alive",
	})
}

func (h *Handlers) Readyz(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	if err := h.rdb.Ping(ctx).Err(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not ready",
			"reason": "redis unavailable",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ready",
	})
}

func (h *Handlers) Metrics(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(h.startTime).Seconds()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"uptime_seconds": uptime,
		"request_count":  h.requestCount.Load(),
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
