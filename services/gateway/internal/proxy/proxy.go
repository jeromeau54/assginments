package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/go-redis/redis/v8"
)

type Proxy struct {
	pingerURL string
	rdb       *redis.Client
	client    *http.Client
}

func New(pingerHost, pingerPort string, rdb *redis.Client) *Proxy {
	return &Proxy{
		pingerURL: fmt.Sprintf("http://%s:%s", pingerHost, pingerPort),
		rdb:       rdb,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (p *Proxy) ProxyPing(w http.ResponseWriter, r *http.Request) {
	target := fmt.Sprintf("%s/", p.pingerURL)

	resp, err := p.client.Get(target)
	if err != nil {
		log.Printf("Error proxying to pinger: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error":  "pinger unreachable",
			"detail": err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to read pinger response",
		})
		return
	}

	// Cache the result
	ctx := context.Background()
	p.rdb.Set(ctx, "ping:last_response", string(body), 30*time.Second)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func (p *Proxy) CacheGet(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		key = "ping:last_response"
	}

	ctx := context.Background()
	val, err := p.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "key not found",
			"key":   key,
		})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "redis error",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"key":   key,
		"value": val,
	})
}

func (p *Proxy) CacheSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
		return
	}

	ctx := context.Background()
	if err := p.rdb.Set(ctx, req.Key, req.Value, 5*time.Minute).Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to set cache",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "cached",
		"key":    req.Key,
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
