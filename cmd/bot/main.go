package main

import (
	"context"
	"log"
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

	b.Run(ctx)
	log.Println("[main] shutdown complete")
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("env var %s is required", key)
	}
	return v
}
