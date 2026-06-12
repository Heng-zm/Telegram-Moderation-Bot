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

func (b *Bot) issueCaptcha(ctx context.Context, chatID, userID int64, lang string) {
	restrict := tgbotapi.RestrictChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{ChatID: chatID, UserID: userID},
		Permissions: &tgbotapi.ChatPermissions{
			CanSendMessages:       false,
			CanSendMediaMessages:  false,
			CanSendOtherMessages:  false,
			CanAddWebPagePreviews: false,
		},
	}
	if _, err := b.api.Request(restrict); err != nil {
		log.Printf("[captcha] restrict user=%d chat=%d: %v", userID, chatID, err)
	}

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(T(lang, "captcha_button"), captchaCallbackData(chatID, userID)),
		),
	)
	msg := tgbotapi.NewMessage(chatID, T(lang, "captcha_prompt"))
	msg.ReplyMarkup = kb
	sent, err := b.api.Send(msg)
	if err != nil {
		log.Printf("[captcha] send prompt chat=%d user=%d: %v", chatID, userID, err)
		return
	}

	pending := &models.PendingCaptcha{
		ChatID:    chatID,
		UserID:    userID,
		MessageID: sent.MessageID,
		ExpiresAt: time.Now().Add(b.opts.CaptchaTTL),
	}
	b.captchas.Set(pending)
	b.enqueueCaptchaExpiry(ctx, chatID, userID, sent.MessageID, pending.ExpiresAt)
}

func (b *Bot) handleCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	if cb == nil || cb.From == nil {
		return
	}
	if cb.Message == nil {
		answerCallback(b.api, cb.ID, "Unsupported callback.", true)
		return
	}

	switch {
	case strings.HasPrefix(cb.Data, "captcha:"):
		b.handleCaptchaCallback(ctx, cb)
	case strings.HasPrefix(cb.Data, "dash:"):
		b.handleDashboardCallback(ctx, cb)
	case strings.HasPrefix(cb.Data, "report:"):
		b.handleReportCallback(ctx, cb)
	case strings.HasPrefix(cb.Data, "botadmin:"):
		b.handleBotAdminCallback(ctx, cb)
	default:
		answerCallback(b.api, cb.ID, "Unknown callback.", true)
	}
}

func (b *Bot) handleCaptchaCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	chatID, expectedUserID, ok := parseCaptchaCallbackData(cb.Data)
	if !ok || chatID != cb.Message.Chat.ID {
		answerCallback(b.api, cb.ID, "Invalid verification button.", true)
		return
	}
	if cb.From.ID != expectedUserID {
		answerCallback(b.api, cb.ID, "This verification is not for you.", true)
		return
	}

	messageID := cb.Message.MessageID
	pending := b.captchas.Get(chatID, expectedUserID)
	cancelledPersistentTask := b.cancelCaptchaExpiry(ctx, chatID, expectedUserID, messageID)
	if pending == nil && !cancelledPersistentTask {
		answerCallback(b.api, cb.ID, "Already verified or expired.", false)
		return
	}
	b.captchas.Delete(chatID, expectedUserID)

	restore := tgbotapi.RestrictChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{ChatID: chatID, UserID: expectedUserID},
		Permissions: &tgbotapi.ChatPermissions{
			CanSendMessages:       true,
			CanSendMediaMessages:  true,
			CanSendOtherMessages:  true,
			CanAddWebPagePreviews: true,
		},
	}
	if _, err := b.api.Request(restore); err != nil {
		log.Printf("[captcha] restore user=%d chat=%d: %v", expectedUserID, chatID, err)
	}

	cfg := b.cache.GetGroup(chatID)
	lang := "en"
	if cfg != nil && cfg.Language != "" {
		lang = cfg.Language
	}
	edit := tgbotapi.NewEditMessageText(chatID, messageID, T(lang, "captcha_verified"))
	if _, err := b.api.Request(edit); err != nil {
		log.Printf("[captcha] edit verified msg=%d: %v", messageID, err)
	}
	answerCallback(b.api, cb.ID, "", false)
	b.enqueueDeleteMessage(ctx, chatID, messageID, time.Now().Add(5*time.Second))
}

func captchaCallbackData(chatID, userID int64) string {
	return fmt.Sprintf("captcha:%d:%d", chatID, userID)
}

func parseCaptchaCallbackData(data string) (int64, int64, bool) {
	parts := strings.Split(data, ":")
	if len(parts) != 3 || parts[0] != "captcha" {
		return 0, 0, false
	}
	chatID, err1 := strconv.ParseInt(parts[1], 10, 64)
	userID, err2 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil || chatID == 0 || userID == 0 {
		return 0, 0, false
	}
	return chatID, userID, true
}
