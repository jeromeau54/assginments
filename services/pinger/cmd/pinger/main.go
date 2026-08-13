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
)

func main() {
	cfg := loadConfig()

	rdb := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
	})

	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Printf("WARNING: Redis not available: %v", err)
	} else {
		log.Println("Connected to Redis")
	}

	// Start background pinger
	go runPinger(ctx, cfg, rdb)

	r := mux.NewRouter()
	r.HandleFunc("/", handleRoot).Methods("GET")
	r.HandleFunc("/healthz", handleHealthz).Methods("GET")
	r.HandleFunc("/readyz", handleReadyz).Methods("GET")

	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%s", cfg.Interface, cfg.Port),
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("Pinger listening on %s:%s", cfg.Interface, cfg.Port)
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

func runPinger(ctx context.Context, cfg Config, rdb *redis.Client) {
	targetURL := fmt.Sprintf("%s://%s:%s%s", cfg.TargetProto, cfg.TargetHost, cfg.TargetPort, cfg.TargetPath)
	log.Printf("Pinging %s every second", targetURL)

	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			start := time.Now()
			resp, err := client.Get(targetURL)
			latency := time.Since(start)

			status := "down"
			statusCode := 0
			if err == nil {
				resp.Body.Close()
				statusCode = resp.StatusCode
				if statusCode >= 200 && statusCode < 300 {
					status = "up"
				}
			}

			ready.set("target_up", status == "up")

			log.Printf("Ping %s: status=%s code=%d latency=%s", targetURL, status, statusCode, latency)

			// Cache result in Redis
			result := fmt.Sprintf(`{"status":"%s","code":%d,"latency_ms":%d,"timestamp":"%s"}`,
				status, statusCode, latency.Milliseconds(), time.Now().UTC().Format(time.RFC3339))
			rdb.Set(ctx, "ping:latest", result, 30*time.Second)
			rdb.Incr(ctx, "ping:count")
		}
	}
}
