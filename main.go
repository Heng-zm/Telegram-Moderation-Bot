package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"telemod/internal/bot"
	"telemod/internal/db"
)

const (
	defaultPort           = "8080"
	defaultShutdownPeriod = 10 * time.Second
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.LUTC)

	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("[env] could not load .env: %v", err)
	}

	token := mustEnv("TELEGRAM_BOT_TOKEN")
	dsn := mustEnv("DATABASE_URL")

	rootCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	store, err := db.New(rootCtx, dsn, db.Options{
		MaxConns: parseIntEnv("DB_MAX_CONNS", 20, 1, 200),
		MinConns: parseIntEnv("DB_MIN_CONNS", 2, 0, 50),
	})
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer store.Close()

	telegramBot, err := bot.New(token, store, bot.Options{
		BotOwnerIDs:             parseInt64ListEnv("BOT_OWNER_IDS"),
		BotOwnerCanManageGroups: parseBoolEnv("BOT_OWNER_CAN_MANAGE_GROUPS", false),
		UpdateWorkers:           parseIntEnv("BOT_UPDATE_WORKERS", 32, 1, 512),
		UpdateQueueSize:         parseIntEnv("BOT_UPDATE_QUEUE_SIZE", 2048, 16, 100000),
		AuditQueueSize:          parseIntEnv("BOT_AUDIT_QUEUE_SIZE", 1024, 16, 100000),
		MetricQueueSize:         parseIntEnv("BOT_METRIC_QUEUE_SIZE", 2048, 16, 100000),
		AdminCacheTTL:           parseDurationEnv("BOT_ADMIN_CACHE_TTL", 5*time.Minute),
		CaptchaTTL:              parseDurationEnv("BOT_CAPTCHA_TTL", 60*time.Second),
		CaptchaNoticeTTL:        parseDurationEnv("BOT_CAPTCHA_NOTICE_TTL", 8*time.Second),
		StrikeWarnTTL:           parseDurationEnv("BOT_STRIKE_WARN_TTL", 5*time.Second),
		DailyReportCron:         stringEnv("BOT_DAILY_REPORT_CRON", "0 0 * * *"),
		FloodLimit:              parseIntEnv("BOT_FLOOD_LIMIT", 5, 1, 100),
		FloodWindow:             parseDurationEnv("BOT_FLOOD_WINDOW", 3*time.Second),
		TaskPollInterval:        parseDurationEnv("BOT_TASK_POLL_INTERVAL", 5*time.Second),
		TaskBatchSize:           parseIntEnv("BOT_TASK_BATCH_SIZE", 25, 1, 500),
		TaskMaxAttempts:         parseIntEnv("BOT_TASK_MAX_ATTEMPTS", 5, 1, 25),
		DeleteWebhookOnStart:    parseBoolEnv("BOT_DELETE_WEBHOOK_ON_START", true),
		DropPendingUpdates:      parseBoolEnv("BOT_DROP_PENDING_UPDATES_ON_START", false),
		DisableDBPollingLock:    parseBoolEnv("BOT_DISABLE_DB_POLLING_LOCK", false),
	})
	if err != nil {
		log.Fatalf("bot: %v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	healthServer := startHealthServer(port, telegramBot, store)

	botErrCh := make(chan error, 1)
	go func() {
		botErrCh <- telegramBot.Run(rootCtx)
	}()

	select {
	case <-rootCtx.Done():
		log.Println("[main] shutdown signal received")
	case err := <-botErrCh:
		if err != nil {
			log.Printf("[main] bot stopped with error: %v", err)
		}
		stopSignals()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownPeriod)
	defer cancel()
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("[health] shutdown error: %v", err)
	}

	log.Println("[main] shutdown complete")
}

func startHealthServer(port string, b *bot.Bot, store *db.Store) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"bot":    b.HealthSnapshot(),
			"db":     store.HealthSnapshot(),
		})
	})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("[health] listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[health] server error: %v", err)
		}
	}()

	return srv
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("[health] encode response: %v", err)
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("env var %s is required", key)
	}
	return v
}

func stringEnv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func parseInt64ListEnv(key string) []int64 {
	raw := os.Getenv(key)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == ';' || r == '\n' || r == '\t'
	})
	out := make([]int64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id == 0 {
			log.Printf("[env] ignoring invalid %s entry %q", key, part)
			continue
		}
		out = append(out, id)
	}
	return out
}

func parseBoolEnv(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		log.Printf("[env] invalid bool %s=%q, using %t", key, raw, fallback)
		return fallback
	}
	return value
}

func parseIntEnv(key string, fallback, minValue, maxValue int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("[env] invalid %s=%q, using %d: %v", key, raw, fallback, err)
		return fallback
	}
	if value < minValue || value > maxValue {
		log.Printf("[env] %s=%d outside [%d,%d], using %d", key, value, minValue, maxValue, fallback)
		return fallback
	}
	return value
}

func parseDurationEnv(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err == nil && value > 0 {
		return value
	}
	if seconds, atoiErr := strconv.Atoi(raw); atoiErr == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	log.Printf("[env] invalid duration %s=%q, using %s", key, raw, fallback)
	return fallback
}
