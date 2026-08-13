package main

import (
	"encoding/json"
	"net/http"
	"sync"
)

type readiness struct {
	mu     sync.RWMutex
	status map[string]bool
}

var ready = &readiness{
	status: map[string]bool{
		"target_up": false,
	},
}

func (rd *readiness) set(key string, val bool) {
	rd.mu.Lock()
	defer rd.mu.Unlock()
	rd.status[key] = val
}

func (rd *readiness) isReady() bool {
	rd.mu.RLock()
	defer rd.mu.RUnlock()
	for _, v := range rd.status {
		if !v {
			return false
		}
	}
	return true
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "hello world",
		"service": "pinger",
	})
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "alive",
	})
}

func handleReadyz(w http.ResponseWriter, r *http.Request) {
	if ready.isReady() {
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "ready",
		})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{
		"status": "not ready",
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
