package bot

import (
	"context"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// handleUpdate is the top-level dispatcher. Each update runs in its own
// goroutine so no message blocks any other.
func (b *Bot) handleUpdate(ctx context.Context, upd tgbotapi.Update) {
	switch {
	case upd.CallbackQuery != nil:
		b.handleCallback(ctx, upd.CallbackQuery)

	case upd.Message != nil:
		b.handleMessage(ctx, upd.Message)
	}
}

// handleMessage implements the decision flow from the spec.
func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	// ── New member join ───────────────────────────────────────────────────────
	if msg.NewChatMembers != nil {
		cfg, err := b.ensureGroup(ctx, msg.Chat.ID)
		if err != nil {
			log.Printf("[handler] ensure group %d: %v", msg.Chat.ID, err)
			return
		}
		if cfg.CaptchaEnabled {
			for _, member := range msg.NewChatMembers {
				if member.IsBot {
					continue
				}
				b.issueCaptcha(ctx, msg.Chat.ID, member.ID, cfg.Language)
			}
		}
		return
	}

	// Private DM → admin dashboard.
	if msg.Chat.IsPrivate() {
		b.handleDM(ctx, msg)
		return
	}

	// Group message pipeline.
	cfg, err := b.ensureGroup(ctx, msg.Chat.ID)
	if err != nil {
		log.Printf("[handler] ensure group %d: %v", msg.Chat.ID, err)
		return
	}

	// ── Admin commands ────────────────────────────────────────────────────────
	if msg.From != nil && b.isAdmin(ctx, msg.Chat.ID, msg.From.ID) {
		if msg.IsCommand() {
			b.handleAdminCommand(ctx, msg, cfg)
			return
		}
	}

	// ── Regular user security pipeline ───────────────────────────────────────
	if msg.From == nil {
		return
	}
	b.runSecurityChecks(ctx, msg, cfg)
}
