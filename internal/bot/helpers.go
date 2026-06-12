package bot

import (
	"context"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telemod/internal/models"
)

// isAdmin queries the Telegram API to check whether userID is a group admin.
// Errors (e.g. bot not in group) are treated as non-admin.
func (b *Bot) isAdmin(ctx context.Context, chatID, userID int64) bool {
	cm, err := b.api.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
			ChatID: chatID,
			UserID: userID,
		},
	})
	if err != nil {
		return false
	}
	return cm.IsAdministrator() || cm.IsCreator()
}

// ensureGroup returns the group config from cache, falling back to the DB
// and then creating a default record for first-time groups.
func (b *Bot) ensureGroup(ctx context.Context, chatID int64) (*models.Group, error) {
	if cfg := b.cache.GetGroup(chatID); cfg != nil {
		return cfg, nil
	}

	cfg, err := b.store.GetOrCreateGroup(ctx, chatID)
	if err != nil {
		return nil, err
	}
	b.cache.SetGroup(cfg)

	// Warm up bad words and whitelist caches in the background.
	go func() {
		bw, err := b.store.GetBadWords(ctx, chatID)
		if err != nil {
			log.Printf("[cache] warm bad words %d: %v", chatID, err)
			return
		}
		b.cache.SetBadWords(chatID, bw)

		wl, err := b.store.GetWhitelist(ctx, chatID)
		if err != nil {
			log.Printf("[cache] warm whitelist %d: %v", chatID, err)
			return
		}
		b.cache.SetWhitelist(chatID, wl)
	}()

	return cfg, nil
}
