package bot

import (
	"context"
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telemod/internal/models"
)

// issueCaptcha mutes a new member, sends a verification button, and starts
// a 60-second expiry timer in a separate goroutine.
func (b *Bot) issueCaptcha(ctx context.Context, chatID, userID int64, lang string) {
	// Mute the user immediately.
	restrict := tgbotapi.RestrictChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{
			ChatID: chatID,
			UserID: userID,
		},
		Permissions: &tgbotapi.ChatPermissions{
			CanSendMessages:       false,
			CanSendMediaMessages:  false,
			CanSendOtherMessages:  false,
			CanAddWebPagePreviews: false,
		},
	}
	if _, err := b.api.Request(restrict); err != nil {
		log.Printf("[captcha] restrict %d in %d: %v", userID, chatID, err)
		// Non-fatal – continue so the user at least sees the prompt.
	}

	// Send CAPTCHA inline keyboard.
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				T(lang, "captcha_button"),
				captchaCallbackData(chatID, userID),
			),
		),
	)
	msg := tgbotapi.NewMessage(chatID, T(lang, "captcha_prompt"))
	msg.ReplyMarkup = kb
	sent, err := b.api.Send(msg)
	if err != nil {
		log.Printf("[captcha] send prompt: %v", err)
		return
	}

	// Track in concurrent store.
	pending := &models.PendingCaptcha{
		ChatID:    chatID,
		UserID:    userID,
		MessageID: sent.MessageID,
		ExpiresAt: time.Now().Add(60 * time.Second),
	}
	b.captchas.Set(pending)

	// Non-blocking expiry worker.
	go func() {
		time.Sleep(60 * time.Second)
		p := b.captchas.Get(chatID, userID)
		if p == nil {
			// Already verified – nothing to do.
			return
		}
		b.captchas.Delete(chatID, userID)

		// Delete the CAPTCHA message.
		b.api.Request(tgbotapi.NewDeleteMessage(chatID, p.MessageID)) //nolint

		// Ban the unverified user.
		ban := tgbotapi.BanChatMemberConfig{
			ChatMemberConfig: tgbotapi.ChatMemberConfig{
				ChatID: chatID,
				UserID: userID,
			},
		}
		if _, err := b.api.Request(ban); err != nil {
			log.Printf("[captcha] ban expired user %d: %v", userID, err)
		}

		// Notify in chat (brief expiry notice).
		cfg := b.cache.GetGroup(chatID)
		l := "en"
		if cfg != nil {
			l = cfg.Language
		}
		note := tgbotapi.NewMessage(chatID, T(l, "captcha_expired"))
		sent, _ := b.api.Send(note)
		go func() {
			time.Sleep(8 * time.Second)
			b.api.Request(tgbotapi.NewDeleteMessage(chatID, sent.MessageID)) //nolint
		}()
	}()
}

// handleCallback processes inline button taps including CAPTCHA verification.
func (b *Bot) handleCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	if cb.Message == nil || cb.From == nil {
		return
	}

	chatID := cb.Message.Chat.ID
	userID := cb.From.ID

	if !isCaptchaCallback(cb.Data, chatID, userID) {
		// Could be a dashboard button – route there.
		b.handleDashboardCallback(ctx, cb)
		return
	}

	// Verify the user.
	pending := b.captchas.Get(chatID, userID)
	if pending == nil {
		b.api.Request(tgbotapi.NewCallback(cb.ID, "Already verified or expired.")) //nolint
		return
	}
	b.captchas.Delete(chatID, userID)

	// Restore permissions.
	restore := tgbotapi.RestrictChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{
			ChatID: chatID,
			UserID: userID,
		},
		Permissions: &tgbotapi.ChatPermissions{
			CanSendMessages:       true,
			CanSendMediaMessages:  true,
			CanSendOtherMessages:  true,
			CanAddWebPagePreviews: true,
		},
	}
	if _, err := b.api.Request(restore); err != nil {
		log.Printf("[captcha] restore %d: %v", userID, err)
	}

	// Edit the CAPTCHA message to a success notice.
	cfg := b.cache.GetGroup(chatID)
	l := "en"
	if cfg != nil {
		l = cfg.Language
	}
	edit := tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, T(l, "captcha_verified"))
	b.api.Send(edit) //nolint
	b.api.Request(tgbotapi.NewCallback(cb.ID, "")) //nolint

	// Auto-delete success message after 5 s.
	go func() {
		time.Sleep(5 * time.Second)
		b.api.Request(tgbotapi.NewDeleteMessage(chatID, cb.Message.MessageID)) //nolint
	}()
}

// captchaCallbackData encodes a unique callback identifier for a CAPTCHA.
func captchaCallbackData(chatID, userID int64) string {
	return "captcha:" + itoa(chatID) + ":" + itoa(userID)
}

// isCaptchaCallback checks that the callback data matches the expected format
// for the given chat/user pair.
func isCaptchaCallback(data string, chatID, userID int64) bool {
	return data == captchaCallbackData(chatID, userID)
}
