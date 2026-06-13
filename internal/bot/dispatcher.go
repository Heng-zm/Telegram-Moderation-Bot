package bot

import (
	"context"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) handleUpdate(ctx context.Context, upd tgbotapi.Update) {
	switch {
	case upd.CallbackQuery != nil:
		b.handleCallback(ctx, upd.CallbackQuery)
	case upd.Message != nil:
		b.handleMessage(ctx, upd.Message)
	case upd.EditedMessage != nil:
		b.handleMessage(ctx, upd.EditedMessage)
	case upd.ChannelPost != nil:
		b.handleChannelPost(ctx, upd.ChannelPost)
	case upd.EditedChannelPost != nil:
		b.handleChannelPost(ctx, upd.EditedChannelPost)
	}
}

func (b *Bot) handleChannelPost(ctx context.Context, msg *tgbotapi.Message) {
	if msg == nil || msg.Chat == nil {
		return
	}
	if _, err := b.ensureGroup(ctx, msg.Chat.ID); err != nil {
		log.Printf("[handler] ensure channel %d: %v", msg.Chat.ID, err)
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	if msg == nil || msg.Chat == nil {
		return
	}

	if len(msg.NewChatMembers) > 0 {
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

	if msg.Chat.IsPrivate() {
		if b.handleBotOwnerDM(ctx, msg) {
			return
		}
		b.handleDM(ctx, msg)
		return
	}

	cfg, err := b.ensureGroup(ctx, msg.Chat.ID)
	if err != nil {
		log.Printf("[handler] ensure group %d: %v", msg.Chat.ID, err)
		return
	}

	if msg.IsCommand() && msg.Command() == "report" {
		if b.handleReportCommand(ctx, msg, cfg) {
			return
		}
	}

	if msg.IsCommand() && msg.Command() == "whoami" {
		b.sendWhoAmI(ctx, msg)
		return
	}

	if msg.IsCommand() && isGroupOwnerCommand(msg.Command()) {
		if msg.From == nil || !b.canManageGroupSettings(ctx, msg.Chat.ID, msg.From.ID) {
			sendText(b.api, msg.Chat.ID, T(cfg.Language, "not_group_owner"))
			return
		}
		if b.handleAdminCommand(ctx, msg, cfg) {
			return
		}
	}

	if msg.From == nil {
		return
	}
	b.runSecurityChecks(ctx, msg, cfg)
}
