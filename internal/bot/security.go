package bot

import (
	"context"
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telemod/internal/models"
	"telemod/internal/security"
)

// runSecurityChecks executes the Active Security Processing Loop from the spec:
//  1. Forward & media restriction checks
//  2. Link vs whitelist domain check
//  3. Linguistic word scanner (Fuzzy English / Substring Khmer)
func (b *Bot) runSecurityChecks(ctx context.Context, msg *tgbotapi.Message, cfg *models.Group) {
	reason := ""

	// ── Step 1 · Forward & Media Restrictions ────────────────────────────────
	if cfg.BlockForwards && msg.ForwardFrom != nil {
		reason = models.ViolationForward.String()
	}

	if reason == "" && cfg.BlockMedia {
		if msg.Photo != nil || msg.Voice != nil || msg.Sticker != nil ||
			msg.Animation != nil || msg.Video != nil || msg.Document != nil {
			reason = models.ViolationMedia.String()
		}
	}

	// ── Step 2 · Link Whitelist ───────────────────────────────────────────────
	if reason == "" && cfg.LinksEnabled {
		wl := b.cache.GetWhitelist(cfg.ChatID)
		if wl == nil {
			var err error
			wl, err = b.store.GetWhitelist(ctx, cfg.ChatID)
			if err != nil {
				log.Printf("[security] get whitelist %d: %v", cfg.ChatID, err)
			}
			b.cache.SetWhitelist(cfg.ChatID, wl)
		}
		text := msg.Text + " " + msg.Caption
		if security.ContainsBlockedLink(text, wl) {
			reason = models.ViolationLink.String()
		}
	}

	// ── Step 3 · Linguistic Word Scanner ─────────────────────────────────────
	if reason == "" && cfg.StrikesEnabled {
		bw := b.cache.GetBadWords(cfg.ChatID)
		if bw == nil {
			var err error
			bw, err = b.store.GetBadWords(ctx, cfg.ChatID)
			if err != nil {
				log.Printf("[security] get bad words %d: %v", cfg.ChatID, err)
			}
			b.cache.SetBadWords(cfg.ChatID, bw)
		}
		text := msg.Text + " " + msg.Caption
		if matched, word := security.ScanMessage(text, cfg.Language, bw); matched {
			reason = models.ViolationBadWord.String() + ":" + word
		}
	}

	if reason == "" {
		// Message passes all checks – deliver normally.
		return
	}

	// ── Violation Handling ────────────────────────────────────────────────────
	// 1. Delete the offending message.
	del := tgbotapi.NewDeleteMessage(msg.Chat.ID, msg.MessageID)
	if _, err := b.api.Request(del); err != nil {
		log.Printf("[security] delete msg %d: %v", msg.MessageID, err)
	}

	// 2. Track deletion metric asynchronously.
	go func() {
		if err := b.store.TrackMetric(ctx, cfg.ChatID, "messages_deleted"); err != nil {
			log.Printf("[metric] messages_deleted: %v", err)
		}
	}()

	// 3. Route audit log asynchronously.
	username := ""
	if msg.From != nil {
		username = msg.From.UserName
	}
	b.routeAuditLog(&models.AuditEvent{
		ChatID:    cfg.ChatID,
		UserID:    msg.From.ID,
		Username:  username,
		Content:   msg.Text,
		Reason:    reason,
		Timestamp: time.Now(),
	})

	// 4. Atomically execute the strike engine.
	if cfg.StrikesEnabled {
		b.handleStrike(ctx, msg, cfg)
	}
}

// handleStrike increments the user's strike counter and applies tiered penalties.
func (b *Bot) handleStrike(ctx context.Context, msg *tgbotapi.Message, cfg *models.Group) {
	strikes, err := b.store.IncrementStrike(ctx, cfg.ChatID, msg.From.ID)
	if err != nil {
		log.Printf("[strike] increment: %v", err)
		return
	}

	// Track strike metric out-of-band.
	go func() {
		if err := b.store.TrackMetric(ctx, cfg.ChatID, "strikes_issued"); err != nil {
			log.Printf("[metric] strikes_issued: %v", err)
		}
	}()

	lang := cfg.Language

	switch strikes {
	case 1:
		// Tier 1: Self-destructing warning.
		sent, err := b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, T(lang, "strike1")))
		if err == nil {
			go func() {
				time.Sleep(5 * time.Second)
				b.api.Request(tgbotapi.NewDeleteMessage(msg.Chat.ID, sent.MessageID)) //nolint
			}()
		}

	case 2:
		// Tier 2: 2-hour write suspension.
		until := time.Now().Add(2 * time.Hour).Unix()
		restrict := tgbotapi.RestrictChatMemberConfig{
			ChatMemberConfig: tgbotapi.ChatMemberConfig{
				ChatID: msg.Chat.ID,
				UserID: msg.From.ID,
			},
			UntilDate: until,
			Permissions: &tgbotapi.ChatPermissions{
				CanSendMessages: false,
			},
		}
		if _, err := b.api.Request(restrict); err != nil {
			log.Printf("[strike] restrict user: %v", err)
		}
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, T(lang, "strike2"))) //nolint

	default:
		// Tier 3: Permanent ban + database cleanup.
		ban := tgbotapi.BanChatMemberConfig{
			ChatMemberConfig: tgbotapi.ChatMemberConfig{
				ChatID: msg.Chat.ID,
				UserID: msg.From.ID,
			},
			RevokeMessages: true,
		}
		if _, err := b.api.Request(ban); err != nil {
			log.Printf("[strike] ban user: %v", err)
		}
		// Wipe strike records.
		go func() {
			if err := b.store.DeleteUserStrike(ctx, cfg.ChatID, msg.From.ID); err != nil {
				log.Printf("[strike] delete records: %v", err)
			}
			if err := b.store.TrackMetric(ctx, cfg.ChatID, "spammers_kicked"); err != nil {
				log.Printf("[metric] spammers_kicked: %v", err)
			}
		}()
		b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, T(lang, "strike3"))) //nolint
	}
}
