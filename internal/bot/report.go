package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telemod/internal/models"
)

func (b *Bot) handleReportCommand(ctx context.Context, msg *tgbotapi.Message, cfg *models.Group) bool {
	_ = ctx
	if msg == nil || cfg == nil || !msg.IsCommand() || msg.Command() != "report" {
		return false
	}
	if cfg.LogChannelID == 0 {
		sendText(b.api, msg.Chat.ID, T(cfg.Language, "report_no_log_channel"))
		return true
	}
	if msg.ReplyToMessage == nil {
		sendText(b.api, msg.Chat.ID, T(cfg.Language, "report_usage"))
		return true
	}
	if msg.ReplyToMessage.From == nil {
		sendText(b.api, msg.Chat.ID, T(cfg.Language, "report_no_user"))
		return true
	}

	offender := msg.ReplyToMessage.From
	reporter := msg.From
	fwd := tgbotapi.NewForward(cfg.LogChannelID, msg.Chat.ID, msg.ReplyToMessage.MessageID)
	forwarded, err := b.api.Send(fwd)
	if err != nil {
		log.Printf("[report] forward chat=%d msg=%d to log=%d: %v", msg.Chat.ID, msg.ReplyToMessage.MessageID, cfg.LogChannelID, err)
		sendText(b.api, msg.Chat.ID, T(cfg.Language, "report_failed"))
		return true
	}

	text := fmt.Sprintf(
		"🚩 <b>User Report</b>\nGroup: <code>%d</code>\nReporter: <code>%s</code> (<code>%d</code>)\nOffender: <code>%s</code> (<code>%d</code>)\nMessage ID: <code>%d</code>",
		msg.Chat.ID,
		escapeHTML(displayName(reporter)), reporter.ID,
		escapeHTML(displayName(offender)), offender.ID,
		msg.ReplyToMessage.MessageID,
	)
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🚫 Ban User", reportCallbackData("ban", msg.Chat.ID, offender.ID, msg.ReplyToMessage.MessageID)),
			tgbotapi.NewInlineKeyboardButtonData("🗑️ Delete & Strike", reportCallbackData("strike", msg.Chat.ID, offender.ID, msg.ReplyToMessage.MessageID)),
		),
	)
	actionMsg := tgbotapi.NewMessage(cfg.LogChannelID, text)
	actionMsg.ParseMode = tgbotapi.ModeHTML
	actionMsg.ReplyMarkup = kb
	actionMsg.ReplyToMessageID = forwarded.MessageID
	if _, err := b.api.Send(actionMsg); err != nil {
		log.Printf("[report] send action message: %v", err)
	}
	sendText(b.api, msg.Chat.ID, T(cfg.Language, "report_sent"))
	return true
}

func (b *Bot) handleReportCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	if cb == nil || cb.From == nil || cb.Message == nil {
		return
	}
	action, chatID, offenderID, messageID, ok := parseReportCallbackData(cb.Data)
	if !ok {
		answerCallback(b.api, cb.ID, "Invalid report action.", true)
		return
	}
	if !b.isAdmin(ctx, chatID, cb.From.ID) {
		answerCallback(b.api, cb.ID, T("en", "not_group_admin"), true)
		return
	}
	cfg, err := b.ensureGroup(ctx, chatID)
	if err != nil {
		log.Printf("[report] ensure group=%d: %v", chatID, err)
		answerCallback(b.api, cb.ID, "Could not load group.", true)
		return
	}

	switch action {
	case "ban":
		ban := tgbotapi.BanChatMemberConfig{ChatMemberConfig: tgbotapi.ChatMemberConfig{ChatID: chatID, UserID: offenderID}, RevokeMessages: true}
		if _, err := b.api.Request(ban); err != nil {
			log.Printf("[report] ban user=%d chat=%d: %v", offenderID, chatID, err)
			answerCallback(b.api, cb.ID, "Ban failed.", true)
			return
		}
		go func() {
			metricCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := b.store.TrackMetric(metricCtx, chatID, "spammers_kicked"); err != nil {
				log.Printf("[metric] spammers_kicked: %v", err)
			}
		}()
		b.editReportActionMessage(cb, fmt.Sprintf("✅ User <code>%d</code> banned by <code>%s</code>.", offenderID, escapeHTML(displayName(cb.From))))
		answerCallback(b.api, cb.ID, "Banned.", false)
	case "strike":
		b.deleteAndTrack(ctx, chatID, chatID, messageID)
		b.issueStrike(ctx, chatID, offenderID, strconv.FormatInt(offenderID, 10), cfg.Language)
		b.editReportActionMessage(cb, fmt.Sprintf("✅ Message deleted and user <code>%d</code> received a strike by <code>%s</code>.", offenderID, escapeHTML(displayName(cb.From))))
		answerCallback(b.api, cb.ID, "Deleted and strike issued.", false)
	default:
		answerCallback(b.api, cb.ID, "Unknown report action.", true)
	}
}

func (b *Bot) editReportActionMessage(cb *tgbotapi.CallbackQuery, text string) {
	edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, text)
	edit.ParseMode = tgbotapi.ModeHTML
	if _, err := b.api.Request(edit); err != nil {
		log.Printf("[report] edit action message: %v", err)
	}
}

func reportCallbackData(action string, chatID, offenderID int64, messageID int) string {
	return fmt.Sprintf("report:%s:%d:%d:%d", action, chatID, offenderID, messageID)
}

func parseReportCallbackData(data string) (string, int64, int64, int, bool) {
	parts := strings.Split(data, ":")
	if len(parts) != 5 || parts[0] != "report" {
		return "", 0, 0, 0, false
	}
	action := parts[1]
	chatID, err1 := strconv.ParseInt(parts[2], 10, 64)
	offenderID, err2 := strconv.ParseInt(parts[3], 10, 64)
	messageID64, err3 := strconv.ParseInt(parts[4], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || chatID == 0 || offenderID == 0 || messageID64 <= 0 {
		return "", 0, 0, 0, false
	}
	return action, chatID, offenderID, int(messageID64), true
}
