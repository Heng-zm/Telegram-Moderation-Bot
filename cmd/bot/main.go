package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"telemod/internal/bot"
	"telemod/internal/db"
)

func main() {
	// Load .env if present (development convenience).
	_ = godotenv.Load()

	token := mustEnv("TELEGRAM_BOT_TOKEN")
	dsn := mustEnv("DATABASE_URL") // Supabase connection string

	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	store, err := db.New(ctx, dsn)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer store.Close()

	b, err := bot.New(token, store)
	if err != nil {
		log.Fatalf("bot: %v", err)
	}

	// Start a minimal HTTP health-check server so Render's port scan
	// succeeds when deployed as a Web Service.
	// PORT is set automatically by Render; fall back to 8080 locally.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	startHealthServer(port)

	b.Run(ctx)
	log.Println("[main] shutdown complete")
}

// startHealthServer launches a tiny HTTP server in the background.
// GET /healthz → 200 OK   (Render health-check endpoint)
// GET /         → 200 OK   (satisfies the initial port scan)
func startHealthServer(port string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	go func() {
		log.Printf("[health] listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[health] server error: %v", err)
		}
	}()
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("env var %s is required", key)
	}
	return v
}
