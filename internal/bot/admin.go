package bot

import (
	"context"
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telemod/internal/models"
)

// ── Emergency Kill Switch ─────────────────────────────────────────────────────

// handleAdminCommand dispatches /lock and /unlock commands.
func (b *Bot) handleAdminCommand(ctx context.Context, msg *tgbotapi.Message, cfg *models.Group) {
	switch msg.Command() {
	case "lock":
		b.setGroupLock(ctx, msg.Chat.ID, cfg.Language, false)
	case "unlock":
		b.setGroupLock(ctx, msg.Chat.ID, cfg.Language, true)
	}
}

// setGroupLock applies or lifts the chat-wide write prohibition.
// SetChatPermissionsConfig embeds ChatConfig, which holds the ChatID.
func (b *Bot) setGroupLock(ctx context.Context, chatID int64, lang string, allow bool) {
	perms := &tgbotapi.ChatPermissions{
		CanSendMessages:       allow,
		CanSendMediaMessages:  allow,
		CanSendOtherMessages:  allow,
		CanAddWebPagePreviews: allow,
	}
	sc := tgbotapi.SetChatPermissionsConfig{
		ChatConfig:  tgbotapi.ChatConfig{ChatID: chatID},
		Permissions: perms,
	}
	if _, err := b.api.Request(sc); err != nil {
		log.Printf("[lock] set permissions on %d: %v", chatID, err)
		return
	}
	key := "chat_locked"
	if allow {
		key = "chat_unlocked"
	}
	b.api.Send(tgbotapi.NewMessage(chatID, T(lang, key))) //nolint
}

// ── Admin DM Dashboard ────────────────────────────────────────────────────────

// handleDM routes private messages to the interactive settings dashboard.
func (b *Bot) handleDM(ctx context.Context, msg *tgbotapi.Message) {
	if msg.IsCommand() && msg.Command() == "settings" {
		// /settings <chat_id>
		args := strings.Fields(msg.CommandArguments())
		if len(args) < 1 {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "Usage: /settings <chat_id>")) //nolint
			return
		}
		var chatID int64
		fmt.Sscanf(args[0], "%d", &chatID)
		if chatID == 0 {
			return
		}
		// Verify the requester is actually an admin of that group.
		if !b.isAdmin(ctx, chatID, msg.From.ID) {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "⛔ You are not an admin of that group.")) //nolint
			return
		}
		cfg, err := b.ensureGroup(ctx, chatID)
		if err != nil {
			log.Printf("[dashboard] ensure group: %v", err)
			return
		}
		b.sendDashboard(ctx, msg.Chat.ID, chatID, cfg)
	}
}

// sendDashboard renders an inline control panel for a group.
func (b *Bot) sendDashboard(ctx context.Context, dmChatID, groupChatID int64, cfg *models.Group) {
	text := fmt.Sprintf("*%s*\nGroup: `%d`\nLanguage: `%s`",
		T(cfg.Language, "dashboard_title"), groupChatID, cfg.Language)

	kb := dashboardKeyboard(groupChatID, cfg)
	msg := tgbotapi.NewMessage(dmChatID, text)
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = kb
	b.api.Send(msg) //nolint
}

// dashboardKeyboard builds the 🟢/🔴 toggle inline keyboard.
func dashboardKeyboard(groupChatID int64, cfg *models.Group) tgbotapi.InlineKeyboardMarkup {
	toggle := func(label string, val bool, cbKey string) tgbotapi.InlineKeyboardButton {
		icon := "🔴"
		if val {
			icon = "🟢"
		}
		data := fmt.Sprintf("dash:%d:%s", groupChatID, cbKey)
		return tgbotapi.NewInlineKeyboardButtonData(icon+" "+label, data)
	}

	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(toggle("CAPTCHA", cfg.CaptchaEnabled, "captcha")),
		tgbotapi.NewInlineKeyboardRow(toggle("Link Filter", cfg.LinksEnabled, "links")),
		tgbotapi.NewInlineKeyboardRow(toggle("Strike Engine", cfg.StrikesEnabled, "strikes")),
		tgbotapi.NewInlineKeyboardRow(toggle("Block Media", cfg.BlockMedia, "media")),
		tgbotapi.NewInlineKeyboardRow(toggle("Block Forwards", cfg.BlockForwards, "forwards")),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🇬🇧 English", fmt.Sprintf("dash:%d:lang:en", groupChatID)),
			tgbotapi.NewInlineKeyboardButtonData("🇰🇭 Khmer", fmt.Sprintf("dash:%d:lang:km", groupChatID)),
		),
	)
}

// handleDashboardCallback processes toggle button taps from the DM dashboard.
func (b *Bot) handleDashboardCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	if !strings.HasPrefix(cb.Data, "dash:") {
		return
	}
	parts := strings.SplitN(cb.Data, ":", 4)
	if len(parts) < 3 {
		return
	}

	var groupChatID int64
	fmt.Sscanf(parts[1], "%d", &groupChatID)
	action := parts[2]

	cfg := b.cache.GetGroup(groupChatID)
	if cfg == nil {
		var err error
		cfg, err = b.ensureGroup(ctx, groupChatID)
		if err != nil {
			log.Printf("[dashboard] cb ensure group: %v", err)
			return
		}
	}

	// Apply the toggle / language switch.
	b.cache.UpdateGroupField(groupChatID, func(g *models.Group) {
		switch action {
		case "captcha":
			g.CaptchaEnabled = !g.CaptchaEnabled
		case "links":
			g.LinksEnabled = !g.LinksEnabled
		case "strikes":
			g.StrikesEnabled = !g.StrikesEnabled
		case "media":
			g.BlockMedia = !g.BlockMedia
		case "forwards":
			g.BlockForwards = !g.BlockForwards
		case "lang":
			if len(parts) == 4 {
				g.Language = parts[3]
			}
		}
		cfg = g
	})

	// Persist to Supabase.
	go func() {
		if err := b.store.UpdateGroup(ctx, cfg); err != nil {
			log.Printf("[dashboard] persist group: %v", err)
		}
	}()

	// Rewrite the inline keyboard in-place (hot state sync).
	refreshed := dashboardKeyboard(groupChatID, cfg)
	edit := tgbotapi.NewEditMessageReplyMarkup(
		cb.Message.Chat.ID,
		cb.Message.MessageID,
		refreshed,
	)
	b.api.Request(edit)                              //nolint
	b.api.Request(tgbotapi.NewCallback(cb.ID, "✅")) //nolint
}
