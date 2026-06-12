package bot

import (
	"context"
	"html"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telemod/internal/models"
)

func (b *Bot) ensureGroup(ctx context.Context, chatID int64) (*models.Group, error) {
	if cfg := b.cache.GetGroup(chatID); cfg != nil {
		return cfg, nil
	}
	cfg, err := b.store.GetOrCreateGroup(ctx, chatID)
	if err != nil {
		return nil, err
	}
	b.cache.SetGroup(cfg)
	b.warmGroupLists(chatID)
	return cfg, nil
}

func (b *Bot) warmGroupLists(chatID int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		bw, err := b.store.GetBadWords(ctx, chatID)
		if err != nil {
			log.Printf("[cache] warm bad words %d: %v", chatID, err)
		} else {
			b.cache.SetBadWords(chatID, bw)
		}

		wl, err := b.store.GetWhitelist(ctx, chatID)
		if err != nil {
			log.Printf("[cache] warm whitelist %d: %v", chatID, err)
		} else {
			b.cache.SetWhitelist(chatID, wl)
		}
	}()
}

func sendText(api *tgbotapi.BotAPI, chatID int64, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if _, err := api.Send(tgbotapi.NewMessage(chatID, text)); err != nil {
		log.Printf("[telegram] send chat=%d: %v", chatID, err)
	}
}

func sendHTML(api *tgbotapi.BotAPI, chatID int64, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	if _, err := api.Send(msg); err != nil {
		log.Printf("[telegram] send html chat=%d: %v", chatID, err)
	}
}

func answerCallback(api *tgbotapi.BotAPI, callbackID, text string, alert bool) {
	if callbackID == "" {
		return
	}
	cfg := tgbotapi.CallbackConfig{CallbackQueryID: callbackID, Text: text, ShowAlert: alert}
	if _, err := api.Request(cfg); err != nil {
		log.Printf("[telegram] answer callback: %v", err)
	}
}

func messageContent(msg *tgbotapi.Message) string {
	if msg == nil {
		return ""
	}
	parts := []string{msg.Text, msg.Caption}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func escapeHTML(s string) string { return html.EscapeString(s) }

func displayName(user *tgbotapi.User) string {
	if user == nil {
		return ""
	}
	if user.UserName != "" {
		return "@" + user.UserName
	}
	name := strings.TrimSpace(strings.Join([]string{user.FirstName, user.LastName}, " "))
	if name != "" {
		return name
	}
	return strconv.FormatInt(user.ID, 10)
}
