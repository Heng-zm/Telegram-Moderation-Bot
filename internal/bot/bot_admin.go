package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) handleBotOwnerDM(ctx context.Context, msg *tgbotapi.Message) bool {
	if msg == nil || msg.Chat == nil || msg.From == nil || !msg.Chat.IsPrivate() || !msg.IsCommand() {
		return false
	}

	switch msg.Command() {
	case "admin", "botadmin":
		if !b.isBotOwner(msg.From.ID) {
			sendText(b.api, msg.Chat.ID, T("en", "not_bot_owner"))
			return true
		}
		b.sendBotAdminDashboard(ctx, msg.Chat.ID, msg.From.ID)
		return true
	case "groups":
		if !b.isBotOwner(msg.From.ID) {
			sendText(b.api, msg.Chat.ID, T("en", "not_bot_owner"))
			return true
		}
		b.sendBotGroups(ctx, msg.Chat.ID)
		return true
	case "tasks":
		if !b.isBotOwner(msg.From.ID) {
			sendText(b.api, msg.Chat.ID, T("en", "not_bot_owner"))
			return true
		}
		sendHTML(b.api, msg.Chat.ID, b.botTasksText(ctx))
		return true
	case "whoami":
		b.sendWhoAmI(ctx, msg)
		return true
	default:
		return false
	}
}

func (b *Bot) handleBotAdminCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	if cb == nil || cb.From == nil || cb.Message == nil || !strings.HasPrefix(cb.Data, "botadmin:") {
		return
	}
	if !b.isBotOwner(cb.From.ID) {
		answerCallback(b.api, cb.ID, T("en", "not_bot_owner"), true)
		return
	}

	action := strings.TrimPrefix(cb.Data, "botadmin:")
	switch action {
	case "refresh":
		text := b.botAdminDashboardText(cb.From.ID)
		edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, text)
		edit.ParseMode = tgbotapi.ModeHTML
		kb := botAdminKeyboard()
		edit.ReplyMarkup = &kb
		if _, err := b.api.Request(edit); err != nil {
			log.Printf("[bot-admin] refresh edit: %v", err)
		}
		answerCallback(b.api, cb.ID, "Refreshed.", false)
	case "groups":
		text := b.botGroupsText(ctx)
		edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, text)
		edit.ParseMode = tgbotapi.ModeHTML
		kb := botAdminKeyboard()
		edit.ReplyMarkup = &kb
		if _, err := b.api.Request(edit); err != nil {
			log.Printf("[bot-admin] groups edit: %v", err)
		}
		answerCallback(b.api, cb.ID, "Loaded groups.", false)
	case "tasks":
		text := b.botTasksText(ctx)
		edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, text)
		edit.ParseMode = tgbotapi.ModeHTML
		kb := botAdminKeyboard()
		edit.ReplyMarkup = &kb
		if _, err := b.api.Request(edit); err != nil {
			log.Printf("[bot-admin] tasks edit: %v", err)
		}
		answerCallback(b.api, cb.ID, "Loaded tasks.", false)
	default:
		answerCallback(b.api, cb.ID, "Unknown bot-admin action.", true)
	}
}

func (b *Bot) sendBotAdminDashboard(ctx context.Context, chatID, userID int64) {
	_ = ctx
	msg := tgbotapi.NewMessage(chatID, b.botAdminDashboardText(userID))
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = botAdminKeyboard()
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("[bot-admin] send dashboard: %v", err)
	}
}

func botAdminKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 Refresh", "botadmin:refresh"),
			tgbotapi.NewInlineKeyboardButtonData("📚 Groups", "botadmin:groups"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🧰 Tasks", "botadmin:tasks"),
		),
	)
}

func (b *Bot) botAdminDashboardText(userID int64) string {
	snap := b.HealthSnapshot()
	return fmt.Sprintf(
		"🛡️ <b>Bot Admin Center</b>\n"+
			"Your ID: <code>%d</code>\n"+
			"Bot: <code>@%s</code>\n"+
			"Processed updates: <code>%v</code>\n"+
			"Update queue: <code>%v/%v</code>\n"+
			"Audit queue: <code>%v/%v</code>\n"+
			"Metric queue: <code>%v/%v</code>\n"+
			"Daily cron: <code>%s</code>\n"+
			"Group override: <code>%t</code>\n\n"+
			"Bot Admin controls the bot service only. Group/channel settings belong to each Telegram owner/creator.",
		userID,
		escapeHTML(fmt.Sprint(snap["username"])),
		snap["processed_updates"],
		snap["update_queue_len"], snap["update_queue_cap"],
		snap["audit_queue_len"], snap["audit_queue_cap"],
		snap["metric_queue_len"], snap["metric_queue_cap"],
		escapeHTML(fmt.Sprint(snap["daily_report_cron"])),
		b.opts.BotOwnerCanManageGroups,
	)
}

func (b *Bot) sendBotGroups(ctx context.Context, chatID int64) {
	msg := tgbotapi.NewMessage(chatID, b.botGroupsText(ctx))
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = botAdminKeyboard()
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("[bot-admin] send groups: %v", err)
	}
}

func (b *Bot) botGroupsText(ctx context.Context) string {
	groups, err := b.store.GetAllGroups(ctx)
	if err != nil {
		log.Printf("[bot-admin] get groups: %v", err)
		return "❌ Could not load groups."
	}
	if len(groups) == 0 {
		return "📚 <b>Groups</b>\nNo groups registered yet."
	}
	limit := len(groups)
	if limit > 20 {
		limit = 20
	}
	var sb strings.Builder
	sb.WriteString("📚 <b>Registered Groups</b>\n")
	sb.WriteString("Showing latest ")
	sb.WriteString(strconv.Itoa(limit))
	sb.WriteString(" of ")
	sb.WriteString(strconv.Itoa(len(groups)))
	sb.WriteString("\n\n")
	for i := 0; i < limit; i++ {
		g := groups[i]
		sb.WriteString("• <code>")
		sb.WriteString(strconv.FormatInt(g.ChatID, 10))
		sb.WriteString("</code> lang=<code>")
		sb.WriteString(escapeHTML(g.Language))
		sb.WriteString("</code> log=<code>")
		sb.WriteString(strconv.FormatInt(g.LogChannelID, 10))
		sb.WriteString("</code> created=<code>")
		sb.WriteString(escapeHTML(g.CreatedAt.UTC().Format(time.RFC3339)))
		sb.WriteString("</code>\n")
	}
	return sb.String()
}

func (b *Bot) botTasksText(ctx context.Context) string {
	counts, err := b.store.ScheduledTaskCounts(ctx)
	if err != nil {
		log.Printf("[bot-admin] scheduled task counts: %v", err)
		return "❌ Could not load scheduled task counts."
	}
	return fmt.Sprintf(
		"🧰 <b>Scheduled Tasks</b>\n"+
			"Pending: <code>%d</code>\n"+
			"Running: <code>%d</code>\n"+
			"Done: <code>%d</code>\n"+
			"Failed: <code>%d</code>\n"+
			"Cancelled: <code>%d</code>\n\n"+
			"These tasks power restart-safe CAPTCHA expiry, delayed deletes, and unmute actions.",
		counts["pending"],
		counts["running"],
		counts["done"],
		counts["failed"],
		counts["cancelled"],
	)
}

func (b *Bot) sendWhoAmI(ctx context.Context, msg *tgbotapi.Message) {
	if msg == nil || msg.From == nil || msg.Chat == nil {
		return
	}
	roleText := "private"
	if !msg.Chat.IsPrivate() {
		role, ok := b.resolveGroupRole(ctx, msg.Chat.ID, msg.From.ID)
		if !ok {
			roleText = string(roleUnknown)
		} else {
			roleText = string(role)
		}
	}
	text := fmt.Sprintf(
		"👤 <b>Identity</b>\nUser ID: <code>%d</code>\nBot owner: <code>%t</code>\nChat ID: <code>%d</code>\nGroup/channel role: <code>%s</code>",
		msg.From.ID,
		b.isBotOwner(msg.From.ID),
		msg.Chat.ID,
		escapeHTML(roleText),
	)
	sendHTML(b.api, msg.Chat.ID, text)
}
