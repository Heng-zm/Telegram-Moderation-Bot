package bot

import (
	"context"
	"fmt"
	"html"
	"log"
	"runtime/debug"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/robfig/cron/v3"
	"telemod/internal/cache"
	"telemod/internal/db"
	"telemod/internal/models"
)

type Options struct {
	UpdateWorkers    int
	UpdateQueueSize  int
	AuditQueueSize   int
	MetricQueueSize  int
	AdminCacheTTL    time.Duration
	CaptchaTTL       time.Duration
	CaptchaNoticeTTL time.Duration
	StrikeWarnTTL    time.Duration
	DailyReportCron  string
	FloodLimit       int
	FloodWindow      time.Duration
	TaskPollInterval time.Duration
	TaskBatchSize    int
	TaskMaxAttempts  int
}

func (o Options) withDefaults() Options {
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
	return o
}

type Bot struct {
	api      *tgbotapi.BotAPI
	store    *db.Store
	cache    *cache.GroupCache
	captchas *cache.CaptchaStore

	auditCh  chan *models.AuditEvent
	metricCh chan *models.MetricEvent
	updates  chan tgbotapi.Update

	adminCache   *cache.AdminCache
	floodLimiter *cache.FloodLimiter
	scheduler    *cron.Cron
	opts         Options

	processedUpdates uint64
	droppedAudits    uint64
	droppedMetrics   uint64
	processedMetrics uint64
}

func New(token string, store *db.Store, opts Options) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("telegram: create bot api: %w", err)
	}

	opts = opts.withDefaults()
	b := &Bot{
		api:          api,
		store:        store,
		cache:        cache.NewGroupCache(),
		captchas:     cache.NewCaptchaStore(),
		auditCh:      make(chan *models.AuditEvent, opts.AuditQueueSize),
		metricCh:     make(chan *models.MetricEvent, opts.MetricQueueSize),
		updates:      make(chan tgbotapi.Update, opts.UpdateQueueSize),
		adminCache:   cache.NewAdminCache(opts.AdminCacheTTL),
		floodLimiter: cache.NewFloodLimiter(opts.FloodLimit, opts.FloodWindow),
		scheduler:    cron.New(cron.WithLocation(time.UTC), cron.WithChain(cron.Recover(cron.DefaultLogger))),
		opts:         opts,
	}
	return b, nil
}

func (b *Bot) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	b.safeWorker(ctx, &wg, "audit-log", b.auditLogWorker)
	b.safeWorker(ctx, &wg, "metrics", b.metricWorker)
	b.safeWorker(ctx, &wg, "scheduled-task-queue", b.scheduledTaskWorker)
	b.safeWorker(ctx, &wg, "flood-cleanup", b.floodCleanupWorker)

	if _, err := b.scheduler.AddFunc(b.opts.DailyReportCron, func() {
		jobCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		b.sendDailyReports(jobCtx)
	}); err != nil {
		return fmt.Errorf("cron: invalid BOT_DAILY_REPORT_CRON %q: %w", b.opts.DailyReportCron, err)
	}
	b.scheduler.Start()
	defer func() {
		stopCtx := b.scheduler.Stop()
		<-stopCtx.Done()
	}()

	for i := 0; i < b.opts.UpdateWorkers; i++ {
		workerID := i + 1
		b.safeWorker(ctx, &wg, fmt.Sprintf("update-worker-%d", workerID), func(workerCtx context.Context) {
			b.updateWorker(workerCtx, workerID)
		})
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.api.GetUpdatesChan(u)
	defer b.api.StopReceivingUpdates()

	log.Printf("[bot] polling as @%s workers=%d queue=%d daily_cron=%q", b.api.Self.UserName, b.opts.UpdateWorkers, cap(b.updates), b.opts.DailyReportCron)

	for {
		select {
		case <-ctx.Done():
			close(b.updates)
			wg.Wait()
			return nil
		case update, ok := <-updates:
			if !ok {
				cancel()
				close(b.updates)
				wg.Wait()
				return fmt.Errorf("telegram updates channel closed")
			}
			select {
			case b.updates <- update:
			case <-ctx.Done():
				close(b.updates)
				wg.Wait()
				return nil
			}
		}
	}
}

func (b *Bot) updateWorker(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case update, ok := <-b.updates:
			if !ok {
				return
			}
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

func (b *Bot) floodCleanupWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.floodLimiter.CleanupOlderThan(5 * time.Minute)
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
		"username":          b.api.Self.UserName,
		"update_workers":    b.opts.UpdateWorkers,
		"update_queue_len":  len(b.updates),
		"update_queue_cap":  cap(b.updates),
		"audit_queue_len":   len(b.auditCh),
		"audit_queue_cap":   cap(b.auditCh),
		"metric_queue_len":  len(b.metricCh),
		"metric_queue_cap":  cap(b.metricCh),
		"processed_updates": atomic.LoadUint64(&b.processedUpdates),
		"processed_metrics": atomic.LoadUint64(&b.processedMetrics),
		"dropped_audits":    atomic.LoadUint64(&b.droppedAudits),
		"dropped_metrics":   atomic.LoadUint64(&b.droppedMetrics),
		"daily_report_cron": b.opts.DailyReportCron,
		"flood_limit":       b.opts.FloodLimit,
		"flood_window":      b.opts.FloodWindow.String(),
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
