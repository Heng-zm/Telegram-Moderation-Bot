package bot

import (
	"context"
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telemod/internal/cache"
	"telemod/internal/db"
	"telemod/internal/models"
)

// Bot is the root coordinator that owns the Telegram client, DB store,
// caches, and the async audit-log channel.
type Bot struct {
	api       *tgbotapi.BotAPI
	store     *db.Store
	cache     *cache.GroupCache
	captchas  *cache.CaptchaStore
	auditCh   chan *models.AuditEvent
}

// New creates a fully wired Bot.
func New(token string, store *db.Store) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}
	b := &Bot{
		api:      api,
		store:    store,
		cache:    cache.NewGroupCache(),
		captchas: cache.NewCaptchaStore(),
		auditCh:  make(chan *models.AuditEvent, 512),
	}
	return b, nil
}

// Run starts background workers and the main update polling loop.
func (b *Bot) Run(ctx context.Context) {
	// Start async audit log router.
	go b.auditLogWorker(ctx)

	// Start 24-hour daily stats cron.
	go b.dailyStatsCron(ctx)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.api.GetUpdatesChan(u)

	log.Printf("[bot] polling as @%s", b.api.Self.UserName)

	for {
		select {
		case <-ctx.Done():
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			go b.handleUpdate(ctx, update)
		}
	}
}

// ── Background Workers ────────────────────────────────────────────────────────

// auditLogWorker drains the audit channel and forwards events to the group's
// configured log channel. Runs entirely out-of-band.
func (b *Bot) auditLogWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-b.auditCh:
			cfg := b.cache.GetGroup(ev.ChatID)
			if cfg == nil || cfg.LogChannelID == 0 {
				continue
			}
			text := formatAuditEvent(ev)
			msg := tgbotapi.NewMessage(cfg.LogChannelID, text)
			msg.ParseMode = tgbotapi.ModeMarkdown
			if _, err := b.api.Send(msg); err != nil {
				log.Printf("[audit] send error: %v", err)
			}
		}
	}
}

// dailyStatsCron fires every 24 h and sends a stats digest to each group's
// log channel.
func (b *Bot) dailyStatsCron(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.sendDailyReports(ctx)
		}
	}
}

// sendDailyReports fetches today's stats and dispatches per-group messages.
func (b *Bot) sendDailyReports(ctx context.Context) {
	stats, err := b.store.GetDailyStats(ctx)
	if err != nil {
		log.Printf("[cron] get daily stats: %v", err)
		return
	}
	for _, gs := range stats {
		cfg := b.cache.GetGroup(gs.ChatID)
		if cfg == nil || cfg.LogChannelID == 0 {
			continue
		}
		lang := cfg.Language
		text := formatDailyReport(lang, gs)
		msg := tgbotapi.NewMessage(cfg.LogChannelID, text)
		msg.ParseMode = tgbotapi.ModeMarkdown
		if _, err := b.api.Send(msg); err != nil {
			log.Printf("[cron] send report to %d: %v", cfg.LogChannelID, err)
		}
	}
}

// routeAuditLog enqueues an audit event non-blocking. If the channel is full
// the event is dropped to avoid blocking the message processing goroutine.
func (b *Bot) routeAuditLog(ev *models.AuditEvent) {
	select {
	case b.auditCh <- ev:
	default:
		log.Printf("[audit] channel full – dropping event for chat %d", ev.ChatID)
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func formatAuditEvent(ev *models.AuditEvent) string {
	return "🚨 *Violation Detected*\n" +
		"User: `" + ev.Username + "` (`" + itoa(ev.UserID) + "`)\n" +
		"Reason: `" + ev.Reason + "`\n" +
		"Content: `" + truncate(ev.Content, 200) + "`\n" +
		"Time: " + ev.Timestamp.UTC().Format(time.RFC3339)
}

func formatDailyReport(lang string, gs *models.GroupStats) string {
	return "*" + T(lang, "daily_report") + "*\n" +
		"📅 " + gs.RecordDate.Format("2006-01-02") + "\n" +
		"🗑️ " + T(lang, "msg_deleted") + ": `" + itoa(int64(gs.MessagesDeleted)) + "`\n" +
		"🚫 " + T(lang, "spammers_kicked") + ": `" + itoa(int64(gs.SpammersKicked)) + "`\n" +
		"⚡ " + T(lang, "strikes_issued") + ": `" + itoa(int64(gs.StrikesIssued)) + "`"
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n >= 10 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	pos--
	buf[pos] = byte('0' + n)
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
