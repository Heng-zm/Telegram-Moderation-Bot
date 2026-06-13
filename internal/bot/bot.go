package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/robfig/cron/v3"
	"telemod/internal/cache"
	"telemod/internal/db"
	"telemod/internal/models"
)

const (
	RuntimeModePolling = "polling"
	RuntimeModeWebhook = "webhook"
)

type Options struct {
	BotOwnerIDs             []int64
	BotOwnerCanManageGroups bool
	RuntimeMode             string
	WebhookURL              string
	WebhookPath             string
	WebhookSecretToken      string
	SetWebhookOnStart       bool
	WebhookMaxBodyBytes     int64
	UpdateWorkers           int
	UpdateQueueSize         int
	AuditQueueSize          int
	MetricQueueSize         int
	AdminCacheTTL           time.Duration
	CaptchaTTL              time.Duration
	CaptchaNoticeTTL        time.Duration
	StrikeWarnTTL           time.Duration
	DailyReportCron         string
	FloodLimit              int
	FloodWindow             time.Duration
	TaskPollInterval        time.Duration
	TaskBatchSize           int
	TaskMaxAttempts         int
	TaskCleanupAge          time.Duration
	TaskCleanupInterval     time.Duration
	DeleteWebhookOnStart    bool
	DropPendingUpdates      bool
	DisableDBPollingLock    bool
	ExemptAdmins            bool
	ReportCooldown          time.Duration
}

func (o Options) withDefaults() Options {
	o.RuntimeMode = strings.ToLower(strings.TrimSpace(o.RuntimeMode))
	if o.RuntimeMode == "" {
		o.RuntimeMode = RuntimeModePolling
	}
	if o.WebhookPath == "" {
		o.WebhookPath = "/tg-webhook"
	}
	if !strings.HasPrefix(o.WebhookPath, "/") {
		o.WebhookPath = "/" + o.WebhookPath
	}
	if o.WebhookMaxBodyBytes <= 0 {
		o.WebhookMaxBodyBytes = 2 << 20 // 2 MiB is far above normal Telegram update size.
	}
	if o.UpdateWorkers <= 0 {
		o.UpdateWorkers = 32
	}
	if o.UpdateQueueSize <= 0 {
		o.UpdateQueueSize = 2048
	}
	if o.AuditQueueSize <= 0 {
		o.AuditQueueSize = 1024
	}
	if o.MetricQueueSize <= 0 {
		o.MetricQueueSize = 2048
	}
	if o.AdminCacheTTL <= 0 {
		o.AdminCacheTTL = 5 * time.Minute
	}
	if o.CaptchaTTL <= 0 {
		o.CaptchaTTL = 60 * time.Second
	}
	if o.CaptchaNoticeTTL <= 0 {
		o.CaptchaNoticeTTL = 8 * time.Second
	}
	if o.StrikeWarnTTL <= 0 {
		o.StrikeWarnTTL = 5 * time.Second
	}
	if o.DailyReportCron == "" {
		o.DailyReportCron = "0 0 * * *"
	}
	if o.FloodLimit <= 0 {
		o.FloodLimit = 5
	}
	if o.FloodWindow <= 0 {
		o.FloodWindow = 3 * time.Second
	}
	if o.TaskPollInterval <= 0 {
		o.TaskPollInterval = 5 * time.Second
	}
	if o.TaskBatchSize <= 0 {
		o.TaskBatchSize = 25
	}
	if o.TaskMaxAttempts <= 0 {
		o.TaskMaxAttempts = 5
	}
	if o.TaskCleanupAge <= 0 {
		o.TaskCleanupAge = 72 * time.Hour
	}
	if o.TaskCleanupInterval <= 0 {
		o.TaskCleanupInterval = time.Hour
	}
	if o.ReportCooldown <= 0 {
		o.ReportCooldown = 30 * time.Second
	}
	return o
}

type Bot struct {
	api      *tgbotapi.BotAPI
	token    string
	store    *db.Store
	cache    *cache.GroupCache
	captchas *cache.CaptchaStore

	auditCh  chan *models.AuditEvent
	metricCh chan *models.MetricEvent
	updates  chan tgbotapi.Update

	adminCache    *cache.AdminCache
	floodLimiter  *cache.FloodLimiter
	reportLimiter *cache.CooldownLimiter
	scheduler     *cron.Cron
	opts          Options
	botOwners     map[int64]struct{}

	processedUpdates uint64
	droppedUpdates   uint64
	droppedAudits    uint64
	droppedMetrics   uint64
	processedMetrics uint64
	pollLockKey      int64
}

func New(token string, store *db.Store, opts Options) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("telegram: create bot api: %w", err)
	}

	opts = opts.withDefaults()
	if opts.RuntimeMode != RuntimeModePolling && opts.RuntimeMode != RuntimeModeWebhook {
		return nil, fmt.Errorf("BOT_MODE must be %q or %q, got %q", RuntimeModePolling, RuntimeModeWebhook, opts.RuntimeMode)
	}
	if opts.RuntimeMode == RuntimeModeWebhook {
		if isReservedWebhookPath(opts.WebhookPath) {
			return nil, fmt.Errorf("TELEGRAM_WEBHOOK_PATH %q is reserved; use a private path such as /tg-webhook", opts.WebhookPath)
		}
		if strings.TrimSpace(opts.WebhookSecretToken) == "" {
			return nil, fmt.Errorf("TELEGRAM_WEBHOOK_SECRET_TOKEN is required when BOT_MODE=webhook")
		}
		if len(opts.WebhookSecretToken) < 16 {
			return nil, fmt.Errorf("TELEGRAM_WEBHOOK_SECRET_TOKEN must be at least 16 characters")
		}
		if _, err := normalizeWebhookURL(opts.WebhookURL, opts.WebhookPath); err != nil {
			return nil, err
		}
	}

	owners := make(map[int64]struct{}, len(opts.BotOwnerIDs))
	for _, id := range opts.BotOwnerIDs {
		if id != 0 {
			owners[id] = struct{}{}
		}
	}

	b := &Bot{
		api:           api,
		token:         token,
		store:         store,
		cache:         cache.NewGroupCache(),
		captchas:      cache.NewCaptchaStore(),
		auditCh:       make(chan *models.AuditEvent, opts.AuditQueueSize),
		metricCh:      make(chan *models.MetricEvent, opts.MetricQueueSize),
		updates:       make(chan tgbotapi.Update, opts.UpdateQueueSize),
		adminCache:    cache.NewAdminCache(opts.AdminCacheTTL),
		floodLimiter:  cache.NewFloodLimiter(opts.FloodLimit, opts.FloodWindow),
		reportLimiter: cache.NewCooldownLimiter(opts.ReportCooldown),
		scheduler:     cron.New(cron.WithLocation(time.UTC), cron.WithChain(cron.Recover(cron.DefaultLogger))),
		opts:          opts,
		botOwners:     owners,
		pollLockKey:   defaultPollingLockKey(api.Self.ID),
	}
	return b, nil
}

func (b *Bot) Run(ctx context.Context) error {
	if b.opts.RuntimeMode == RuntimeModeWebhook {
		return b.runWebhook(ctx)
	}
	return b.runPolling(ctx)
}

func (b *Bot) runPolling(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pollLock, err := b.acquirePollingLock(ctx)
	if err != nil {
		return err
	}
	if pollLock != nil {
		defer func() {
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer releaseCancel()
			if err := pollLock.Release(releaseCtx); err != nil {
				log.Printf("[bot] release polling lock: %v", err)
			}
		}()
	}

	if err := b.preparePolling(ctx); err != nil {
		return err
	}

	var wg sync.WaitGroup
	if err := b.startBackgroundWorkers(ctx, &wg); err != nil {
		return err
	}
	defer b.stopBackgroundWorkers(cancel, &wg)

	log.Printf("[bot] polling as @%s workers=%d queue=%d daily_cron=%q db_lock=%t", b.api.Self.UserName, b.opts.UpdateWorkers, cap(b.updates), b.opts.DailyReportCron, pollLock != nil)
	return b.pollUpdates(ctx)
}

func (b *Bot) runWebhook(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if b.opts.SetWebhookOnStart {
		if err := b.setWebhook(ctx); err != nil {
			return err
		}
	}

	var wg sync.WaitGroup
	if err := b.startBackgroundWorkers(ctx, &wg); err != nil {
		return err
	}
	defer b.stopBackgroundWorkers(cancel, &wg)

	webhookURL, _ := normalizeWebhookURL(b.opts.WebhookURL, b.opts.WebhookPath)
	log.Printf("[bot] webhook as @%s path=%q url=%q workers=%d queue=%d daily_cron=%q", b.api.Self.UserName, b.opts.WebhookPath, webhookURL, b.opts.UpdateWorkers, cap(b.updates), b.opts.DailyReportCron)
	<-ctx.Done()
	return nil
}

func (b *Bot) startBackgroundWorkers(ctx context.Context, wg *sync.WaitGroup) error {
	if _, err := b.scheduler.AddFunc(b.opts.DailyReportCron, func() {
		jobCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		b.sendDailyReports(jobCtx)
	}); err != nil {
		return fmt.Errorf("cron: invalid BOT_DAILY_REPORT_CRON %q: %w", b.opts.DailyReportCron, err)
	}

	b.safeWorker(ctx, wg, "audit-log", b.auditLogWorker)
	b.safeWorker(ctx, wg, "metrics", b.metricWorker)
	b.safeWorker(ctx, wg, "scheduled-task-queue", b.scheduledTaskWorker)
	b.safeWorker(ctx, wg, "scheduled-task-cleanup", b.scheduledTaskCleanupWorker)
	b.safeWorker(ctx, wg, "memory-cleanup", b.memoryCleanupWorker)
	b.scheduler.Start()

	for i := 0; i < b.opts.UpdateWorkers; i++ {
		workerID := i + 1
		b.safeWorker(ctx, wg, fmt.Sprintf("update-worker-%d", workerID), func(workerCtx context.Context) {
			b.updateWorker(workerCtx, workerID)
		})
	}
	return nil
}

func (b *Bot) stopBackgroundWorkers(cancel context.CancelFunc, wg *sync.WaitGroup) {
	cancel()
	stopCtx := b.scheduler.Stop()
	<-stopCtx.Done()
	wg.Wait()
}

func (b *Bot) RegisterWebhookHandler(mux *http.ServeMux) error {
	if b.opts.RuntimeMode != RuntimeModeWebhook {
		return nil
	}
	if mux == nil {
		return fmt.Errorf("webhook: nil mux")
	}
	path := b.opts.WebhookPath
	if path == "" || !strings.HasPrefix(path, "/") {
		return fmt.Errorf("TELEGRAM_WEBHOOK_PATH must start with /")
	}
	if isReservedWebhookPath(path) {
		return fmt.Errorf("TELEGRAM_WEBHOOK_PATH %q is reserved", path)
	}
	mux.HandleFunc(path, b.handleWebhookHTTP)
	return nil
}

func (b *Bot) handleWebhookHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWebhookJSON(w, http.StatusMethodNotAllowed, map[string]string{"ok": "false", "error": "method_not_allowed"})
		return
	}
	if b.opts.WebhookSecretToken != "" {
		got := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
		if got == "" || got != b.opts.WebhookSecretToken {
			writeWebhookJSON(w, http.StatusUnauthorized, map[string]string{"ok": "false", "error": "bad_secret"})
			return
		}
	}
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "application/json") {
		writeWebhookJSON(w, http.StatusUnsupportedMediaType, map[string]string{"ok": "false", "error": "content_type_must_be_json"})
		return
	}

	defer r.Body.Close()
	body := http.MaxBytesReader(w, r.Body, b.opts.WebhookMaxBodyBytes)
	dec := json.NewDecoder(body)

	var update tgbotapi.Update
	if err := dec.Decode(&update); err != nil {
		writeWebhookJSON(w, http.StatusBadRequest, map[string]string{"ok": "false", "error": "invalid_update_json"})
		return
	}

	select {
	case b.updates <- update:
		writeWebhookJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case <-r.Context().Done():
		return
	default:
		atomic.AddUint64(&b.droppedUpdates, 1)
		log.Printf("[webhook] update queue full; rejecting update_id=%d", update.UpdateID)
		writeWebhookJSON(w, http.StatusTooManyRequests, map[string]string{"ok": "false", "error": "update_queue_full"})
	}
}

func writeWebhookJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("[webhook] encode response: %v", err)
	}
}

func (b *Bot) setWebhook(ctx context.Context) error {
	webhookURL, err := normalizeWebhookURL(b.opts.WebhookURL, b.opts.WebhookPath)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"url":                  webhookURL,
		"secret_token":         b.opts.WebhookSecretToken,
		"drop_pending_updates": b.opts.DropPendingUpdates,
		"allowed_updates": []string{
			"message",
			"edited_message",
			"channel_post",
			"edited_channel_post",
			"callback_query",
		},
	}
	return b.telegramAPIPost(ctx, "setWebhook", payload)
}

func normalizeWebhookURL(rawURL, path string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("TELEGRAM_WEBHOOK_URL is required when BOT_MODE=webhook")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("TELEGRAM_WEBHOOK_URL must be a full https URL: %q", rawURL)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("TELEGRAM_WEBHOOK_URL must use https, got %q", u.Scheme)
	}
	if strings.TrimSpace(path) == "" {
		path = "/tg-webhook"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = path
	}
	return u.String(), nil
}

func isReservedWebhookPath(path string) bool {
	switch strings.TrimSpace(path) {
	case "", "/", "/livez", "/readyz", "/healthz":
		return true
	default:
		return false
	}
}

func (b *Bot) telegramAPIPost(ctx context.Context, method string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("telegram %s: encode request: %w", method, err)
	}
	endpoint := "https://api.telegram.org/bot" + b.token + "/" + method
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram %s: create request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, 1<<20)
	var apiResp struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		ErrorCode   int    `json:"error_code"`
	}
	if err := json.NewDecoder(limited).Decode(&apiResp); err != nil {
		return fmt.Errorf("telegram %s: decode response status=%d: %w", method, resp.StatusCode, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !apiResp.OK {
		if apiResp.Description == "" {
			apiResp.Description = resp.Status
		}
		return fmt.Errorf("telegram %s failed status=%d code=%d: %s", method, resp.StatusCode, apiResp.ErrorCode, apiResp.Description)
	}
	log.Printf("[bot] telegram %s ok", method)
	return nil
}

func (b *Bot) acquirePollingLock(ctx context.Context) (*db.AdvisoryLock, error) {
	if b.opts.DisableDBPollingLock {
		log.Printf("[bot] DB polling lock disabled by BOT_DISABLE_DB_POLLING_LOCK=true")
		return nil, nil
	}

	lockCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	lock, acquired, err := b.store.TryAcquireAdvisoryLock(lockCtx, b.pollLockKey)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, fmt.Errorf("another Telemod instance is already running for this bot/database; stop the duplicate Render service/local process or set a different TELEGRAM_BOT_TOKEN")
	}
	log.Printf("[bot] acquired DB polling lock key=%d", b.pollLockKey)
	return lock, nil
}

func (b *Bot) preparePolling(ctx context.Context) error {
	if !b.opts.DeleteWebhookOnStart {
		return nil
	}
	cfg := tgbotapi.DeleteWebhookConfig{DropPendingUpdates: b.opts.DropPendingUpdates}
	if _, err := b.api.Request(cfg); err != nil {
		return fmt.Errorf("telegram: delete webhook before polling: %w", err)
	}
	log.Printf("[bot] webhook cleared before polling drop_pending_updates=%t", b.opts.DropPendingUpdates)
	return nil
}

func (b *Bot) pollUpdates(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		updates, err := b.api.GetUpdates(u)
		if err != nil {
			if isTelegramPollingConflict(err) {
				return fmt.Errorf("telegram getUpdates conflict: another process is polling the same bot token; stop other Render deployments/local bots or use a new TELEGRAM_BOT_TOKEN: %w", err)
			}
			log.Printf("[bot] getUpdates error: %v; retrying in 3 seconds", err)
			if !sleepContext(ctx, 3*time.Second) {
				return nil
			}
			continue
		}

		for _, update := range updates {
			if update.UpdateID >= u.Offset {
				u.Offset = update.UpdateID + 1
			}
			select {
			case b.updates <- update:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

func isTelegramPollingConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "conflict") || strings.Contains(msg, "terminated by other getupdates") || strings.Contains(msg, "409")
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func defaultPollingLockKey(botID int64) int64 {
	if botID == 0 {
		return 770000000000000001
	}
	return 770000000000000000 + botID
}

func (b *Bot) updateWorker(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case update := <-b.updates:
			b.handleUpdateSafe(ctx, update, workerID)
			atomic.AddUint64(&b.processedUpdates, 1)
		}
	}
}

func (b *Bot) handleUpdateSafe(ctx context.Context, update tgbotapi.Update, workerID int) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[panic] worker=%d update_id=%d panic=%v\n%s", workerID, update.UpdateID, r, debug.Stack())
		}
	}()
	b.handleUpdate(ctx, update)
}

func (b *Bot) safeWorker(ctx context.Context, wg *sync.WaitGroup, name string, fn func(context.Context)) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[panic] worker=%s panic=%v\n%s", name, r, debug.Stack())
			}
		}()
		fn(ctx)
	}()
}

func (b *Bot) auditLogWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-b.auditCh:
			if ev == nil {
				continue
			}
			cfg := b.cache.GetGroup(ev.ChatID)
			if cfg == nil {
				var err error
				cfg, err = b.store.GetGroup(ctx, ev.ChatID)
				if err != nil {
					log.Printf("[audit] get group %d: %v", ev.ChatID, err)
					continue
				}
				b.cache.SetGroup(cfg)
			}
			if cfg.LogChannelID == 0 {
				continue
			}

			msg := tgbotapi.NewMessage(cfg.LogChannelID, formatAuditEvent(ev))
			msg.ParseMode = tgbotapi.ModeHTML
			if _, err := b.api.Send(msg); err != nil {
				log.Printf("[audit] send error: %v", err)
			}
		}
	}
}

func (b *Bot) metricWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-b.metricCh:
			if ev == nil || ev.ChatID == 0 || ev.Column == "" {
				continue
			}
			metricCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := b.store.TrackMetric(metricCtx, ev.ChatID, ev.Column); err != nil {
				log.Printf("[metric] %s chat=%d: %v", ev.Column, ev.ChatID, err)
			} else {
				atomic.AddUint64(&b.processedMetrics, 1)
			}
			cancel()
		}
	}
}

func (b *Bot) trackMetricAsync(chatID int64, column string) {
	if chatID == 0 || column == "" {
		return
	}
	ev := &models.MetricEvent{ChatID: chatID, Column: column}
	select {
	case b.metricCh <- ev:
	default:
		atomic.AddUint64(&b.droppedMetrics, 1)
		log.Printf("[metric] channel full; dropping %s for chat %d", column, chatID)
	}
}

func (b *Bot) memoryCleanupWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.floodLimiter.CleanupOlderThan(5 * time.Minute)
			b.reportLimiter.Cleanup()
		}
	}
}

func (b *Bot) sendDailyReports(ctx context.Context) {
	stats, err := b.store.GetDailyStats(ctx)
	if err != nil {
		log.Printf("[cron] get daily stats: %v", err)
		return
	}
	for _, gs := range stats {
		if gs.LogChannelID == 0 {
			continue
		}
		lang := gs.Language
		if lang == "" {
			lang = "en"
		}
		msg := tgbotapi.NewMessage(gs.LogChannelID, formatDailyReport(lang, gs))
		msg.ParseMode = tgbotapi.ModeHTML
		if _, err := b.api.Send(msg); err != nil {
			log.Printf("[cron] send report to %d: %v", gs.LogChannelID, err)
		}
	}
}

func (b *Bot) routeAuditLog(ev *models.AuditEvent) {
	if ev == nil {
		return
	}
	select {
	case b.auditCh <- ev:
	default:
		atomic.AddUint64(&b.droppedAudits, 1)
		log.Printf("[audit] channel full; dropping event for chat %d", ev.ChatID)
	}
}

func (b *Bot) HealthSnapshot() map[string]any {
	return map[string]any{
		"username":                    b.api.Self.UserName,
		"runtime_mode":                b.opts.RuntimeMode,
		"webhook_path":                b.opts.WebhookPath,
		"set_webhook_on_start":        b.opts.SetWebhookOnStart,
		"webhook_max_body_bytes":      b.opts.WebhookMaxBodyBytes,
		"update_workers":              b.opts.UpdateWorkers,
		"update_queue_len":            len(b.updates),
		"update_queue_cap":            cap(b.updates),
		"audit_queue_len":             len(b.auditCh),
		"audit_queue_cap":             cap(b.auditCh),
		"metric_queue_len":            len(b.metricCh),
		"metric_queue_cap":            cap(b.metricCh),
		"processed_updates":           atomic.LoadUint64(&b.processedUpdates),
		"dropped_updates":             atomic.LoadUint64(&b.droppedUpdates),
		"processed_metrics":           atomic.LoadUint64(&b.processedMetrics),
		"dropped_audits":              atomic.LoadUint64(&b.droppedAudits),
		"dropped_metrics":             atomic.LoadUint64(&b.droppedMetrics),
		"daily_report_cron":           b.opts.DailyReportCron,
		"flood_limit":                 b.opts.FloodLimit,
		"flood_window":                b.opts.FloodWindow.String(),
		"exempt_admins":               b.opts.ExemptAdmins,
		"report_cooldown":             b.opts.ReportCooldown.String(),
		"task_poll_interval":          b.opts.TaskPollInterval.String(),
		"task_batch_size":             b.opts.TaskBatchSize,
		"task_max_attempts":           b.opts.TaskMaxAttempts,
		"task_cleanup_age":            b.opts.TaskCleanupAge.String(),
		"task_cleanup_interval":       b.opts.TaskCleanupInterval.String(),
		"bot_owner_count":             len(b.botOwners),
		"bot_owner_can_manage_groups": b.opts.BotOwnerCanManageGroups,
		"delete_webhook_on_start":     b.opts.DeleteWebhookOnStart,
		"drop_pending_updates":        b.opts.DropPendingUpdates,
		"db_polling_lock_enabled":     b.opts.RuntimeMode == RuntimeModePolling && !b.opts.DisableDBPollingLock,
		"polling_lock_key":            b.pollLockKey,
	}
}

func formatAuditEvent(ev *models.AuditEvent) string {
	return "🚨 <b>Violation Detected</b>\n" +
		"User: <code>" + html.EscapeString(ev.Username) + "</code> (<code>" + strconv.FormatInt(ev.UserID, 10) + "</code>)\n" +
		"Reason: <code>" + html.EscapeString(ev.Reason) + "</code>\n" +
		"Content: <code>" + html.EscapeString(truncate(ev.Content, 200)) + "</code>\n" +
		"Time: " + html.EscapeString(ev.Timestamp.UTC().Format(time.RFC3339))
}

func formatDailyReport(lang string, gs *models.GroupStats) string {
	return "<b>" + html.EscapeString(T(lang, "daily_report")) + "</b>\n" +
		"📅 " + html.EscapeString(gs.RecordDate.Format("2006-01-02")) + "\n" +
		"🗑️ " + html.EscapeString(T(lang, "msg_deleted")) + ": <code>" + strconv.Itoa(gs.MessagesDeleted) + "</code>\n" +
		"🚫 " + html.EscapeString(T(lang, "spammers_kicked")) + ": <code>" + strconv.Itoa(gs.SpammersKicked) + "</code>\n" +
		"⚡ " + html.EscapeString(T(lang, "strikes_issued")) + ": <code>" + strconv.Itoa(gs.StrikesIssued) + "</code>"
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
