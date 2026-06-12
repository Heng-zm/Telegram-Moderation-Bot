package bot

import (
	"context"
	"fmt"
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telemod/internal/models"
	"telemod/internal/security"
)

func (b *Bot) runSecurityChecks(ctx context.Context, msg *tgbotapi.Message, cfg *models.Group) {
	if msg == nil || msg.From == nil || cfg == nil {
		return
	}

	content := messageContent(msg)
	if b.applyFloodLimiter(ctx, msg, cfg, content) {
		return
	}

	reason := ""
	if cfg.BlockForwards && isForwarded(msg) {
		reason = models.ViolationForward.String()
	}

	if reason == "" {
		if blocked, mediaName := blockedMediaReason(msg, cfg); blocked {
			reason = models.ViolationMedia.String() + ":" + mediaName
		}
	}

	if reason == "" && cfg.LinksEnabled {
		wl := b.cache.GetWhitelist(cfg.ChatID)
		if wl == nil {
			loaded, err := b.store.GetWhitelist(ctx, cfg.ChatID)
			if err != nil {
				log.Printf("[security] get whitelist %d: %v", cfg.ChatID, err)
			} else {
				wl = loaded
				b.cache.SetWhitelist(cfg.ChatID, wl)
			}
		}
		if wl != nil && security.ContainsBlockedLink(content, wl) {
			reason = models.ViolationLink.String()
		}
	}

	if reason == "" && cfg.StrikesEnabled {
		bw := b.cache.GetBadWords(cfg.ChatID)
		if bw == nil {
			loaded, err := b.store.GetBadWords(ctx, cfg.ChatID)
			if err != nil {
				log.Printf("[security] get bad words %d: %v", cfg.ChatID, err)
			} else {
				bw = loaded
				b.cache.SetBadWords(cfg.ChatID, bw)
			}
		}
		if bw != nil {
			if matched, word := security.ScanMessage(content, cfg.Language, bw); matched {
				reason = models.ViolationBadWord.String() + ":" + word
			}
		}
	}

	if reason == "" {
		return
	}

	b.deleteAndTrack(ctx, cfg.ChatID, msg.Chat.ID, msg.MessageID)
	b.auditViolation(cfg.ChatID, msg.From.ID, displayName(msg.From), content, reason)

	if cfg.StrikesEnabled {
		b.handleStrike(ctx, msg, cfg)
	}
}

func (b *Bot) applyFloodLimiter(ctx context.Context, msg *tgbotapi.Message, cfg *models.Group, content string) bool {
	decision := b.floodLimiter.Add(msg.Chat.ID, msg.From.ID, msg.MessageID, time.Now())
	if !decision.Flooded {
		return false
	}

	for _, messageID := range decision.MessageIDs {
		b.deleteAndTrack(ctx, cfg.ChatID, msg.Chat.ID, messageID)
	}
	b.auditViolation(cfg.ChatID, msg.From.ID, displayName(msg.From), content, fmt.Sprintf("%s:%d_messages_in_%s", models.ViolationFlood.String(), len(decision.MessageIDs), b.opts.FloodWindow))

	if cfg.StrikesEnabled && decision.ShouldStrike {
		b.issueStrike(ctx, msg.Chat.ID, msg.From.ID, displayName(msg.From), cfg.Language)
	}
	return true
}

func (b *Bot) deleteAndTrack(ctx context.Context, metricChatID, telegramChatID int64, messageID int) {
	if messageID == 0 {
		return
	}
	if _, err := b.api.Request(tgbotapi.NewDeleteMessage(telegramChatID, messageID)); err != nil {
		log.Printf("[security] delete msg=%d chat=%d: %v", messageID, telegramChatID, err)
	}

	b.trackMetricAsync(metricChatID, "messages_deleted")
}

func (b *Bot) auditViolation(chatID, userID int64, username, content, reason string) {
	b.routeAuditLog(&models.AuditEvent{
		ChatID:    chatID,
		UserID:    userID,
		Username:  username,
		Content:   content,
		Reason:    reason,
		Timestamp: time.Now(),
	})
}

func blockedMediaReason(msg *tgbotapi.Message, cfg *models.Group) (bool, string) {
	switch {
	case cfg.BlockPhotos && msg.Photo != nil:
		return true, "photo"
	case cfg.BlockVideos && msg.Video != nil:
		return true, "video"
	case cfg.BlockDocuments && msg.Document != nil:
		return true, "document"
	case cfg.BlockAudio && msg.Audio != nil:
		return true, "audio"
	case cfg.BlockVoice && msg.Voice != nil:
		return true, "voice"
	case cfg.BlockStickers && msg.Sticker != nil:
		return true, "sticker"
	case cfg.BlockAnimations && msg.Animation != nil:
		return true, "animation"
	case cfg.BlockVideoNotes && msg.VideoNote != nil:
		return true, "video_note"
	default:
		return false, ""
	}
}

func isForwarded(msg *tgbotapi.Message) bool {
	return msg.ForwardFrom != nil || msg.ForwardFromChat != nil || msg.ForwardDate != 0 ||
		msg.ForwardSignature != "" || msg.ForwardSenderName != ""
}

func (b *Bot) handleStrike(ctx context.Context, msg *tgbotapi.Message, cfg *models.Group) {
	if msg == nil || msg.From == nil || cfg == nil {
		return
	}
	b.issueStrike(ctx, msg.Chat.ID, msg.From.ID, displayName(msg.From), cfg.Language)
}

func (b *Bot) issueStrike(ctx context.Context, chatID, userID int64, username, lang string) {
	strikes, err := b.store.IncrementStrike(ctx, chatID, userID)
	if err != nil {
		log.Printf("[strike] increment: %v", err)
		return
	}

	b.trackMetricAsync(chatID, "strikes_issued")

	if lang == "" {
		lang = "en"
	}
	switch strikes {
	case 1:
		sent, err := b.api.Send(tgbotapi.NewMessage(chatID, T(lang, "strike1")))
		if err == nil {
			b.enqueueDeleteMessage(ctx, chatID, sent.MessageID, time.Now().Add(b.opts.StrikeWarnTTL))
		} else {
			log.Printf("[strike] send warning: %v", err)
		}
	case 2:
		until := time.Now().Add(2 * time.Hour)
		restrict := tgbotapi.RestrictChatMemberConfig{
			ChatMemberConfig: tgbotapi.ChatMemberConfig{ChatID: chatID, UserID: userID},
			UntilDate:        until.Unix(),
			Permissions:      &tgbotapi.ChatPermissions{CanSendMessages: false},
		}
		if _, err := b.api.Request(restrict); err != nil {
			log.Printf("[strike] restrict user=%d chat=%d: %v", userID, chatID, err)
		}
		b.enqueueUnmuteUser(ctx, chatID, userID, until)
		sendText(b.api, chatID, T(lang, "strike2"))
	default:
		ban := tgbotapi.BanChatMemberConfig{
			ChatMemberConfig: tgbotapi.ChatMemberConfig{ChatID: chatID, UserID: userID},
			RevokeMessages:   true,
		}
		if _, err := b.api.Request(ban); err != nil {
			log.Printf("[strike] ban user=%d chat=%d username=%s: %v", userID, chatID, username, err)
		}
		go func(chatID, userID int64) {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := b.store.DeleteUserStrike(cleanupCtx, chatID, userID); err != nil {
				log.Printf("[strike] delete records: %v", err)
			}
		}(chatID, userID)
		b.trackMetricAsync(chatID, "spammers_kicked")
		sendText(b.api, chatID, T(lang, "strike3"))
	}
}
