package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
	"github.com/govwallet/devops-assessment/services/gateway/internal/handlers"
	"github.com/govwallet/devops-assessment/services/gateway/internal/proxy"
)

func main() {
	port := getEnv("PORT", "8000")
	pingerHost := getEnv("PINGER_HOST", "pinger")
	pingerPort := getEnv("PINGER_PORT", "8000")
	redisHost := getEnv("REDIS_HOST", "redis")
	redisPort := getEnv("REDIS_PORT", "6379")

	rdb := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", redisHost, redisPort),
	})

	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Printf("WARNING: Redis not available: %v", err)
	} else {
		log.Println("Connected to Redis")
	}

	h := handlers.New(rdb)
	p := proxy.New(pingerHost, pingerPort, rdb)

	r := mux.NewRouter()
	r.HandleFunc("/", h.Root).Methods("GET")
	r.HandleFunc("/healthz", h.Healthz).Methods("GET")
	r.HandleFunc("/readyz", h.Readyz).Methods("GET")
	r.HandleFunc("/metrics", h.Metrics).Methods("GET")
	r.HandleFunc("/ping", p.ProxyPing).Methods("GET")
	r.HandleFunc("/cache", p.CacheGet).Methods("GET")
	r.HandleFunc("/cache", p.CacheSet).Methods("POST")

	r.Use(loggingMiddleware)

	srv := &http.Server{
		Addr:         fmt.Sprintf("0.0.0.0:%s", port),
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("Gateway listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down...")
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.RequestURI, time.Since(start))
	})
}
