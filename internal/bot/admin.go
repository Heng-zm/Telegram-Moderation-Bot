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
	"telemod/internal/security"
)

func (b *Bot) handleAdminCommand(ctx context.Context, msg *tgbotapi.Message, cfg *models.Group) bool {
	if msg == nil || cfg == nil {
		return false
	}
	switch msg.Command() {
	case "lock":
		b.setGroupLock(ctx, msg.Chat.ID, cfg.Language, false)
		return true
	case "unlock":
		b.setGroupLock(ctx, msg.Chat.ID, cfg.Language, true)
		return true
	case "settings":
		b.sendDashboard(ctx, msg.Chat.ID, msg.Chat.ID, cfg)
		return true
	case "setlog":
		b.setLogChannel(ctx, msg.Chat.ID, cfg, msg.Chat.ID)
		return true
	case "clearlog":
		b.setLogChannel(ctx, msg.Chat.ID, cfg, 0)
		return true
	case "badword":
		b.handleBadWordCommand(ctx, msg, cfg)
		return true
	case "allowdomain", "allow":
		b.handleAllowDomainCommand(ctx, msg, cfg)
		return true
	case "checkbot":
		b.sendBotPermissionCheck(ctx, msg, cfg)
		return true
	case "status":
		b.sendGroupStatus(ctx, msg, cfg)
		return true
	default:
		return false
	}
}

func isGroupOwnerCommand(command string) bool {
	switch command {
	case "settings", "lock", "unlock", "setlog", "clearlog", "badword", "allowdomain", "allow", "checkbot", "status":
		return true
	default:
		return false
	}
}

func (b *Bot) setGroupLock(ctx context.Context, chatID int64, lang string, allow bool) {
	chat, err := b.api.GetChat(tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: chatID}})
	if err != nil {
		log.Printf("[lock] get chat permissions on %d: %v", chatID, err)
		return
	}

	perms := &tgbotapi.ChatPermissions{}
	if chat.Permissions != nil {
		current := *chat.Permissions
		perms = &current
	}
	perms.CanSendMessages = allow

	req := tgbotapi.SetChatPermissionsConfig{
		ChatConfig:  tgbotapi.ChatConfig{ChatID: chatID},
		Permissions: perms,
	}
	if _, err := b.api.Request(req); err != nil {
		log.Printf("[lock] set permissions on %d: %v", chatID, err)
		return
	}
	key := "chat_locked"
	if allow {
		key = "chat_unlocked"
	}
	sendText(b.api, chatID, T(lang, key))
}

func (b *Bot) handleDM(ctx context.Context, msg *tgbotapi.Message) {
	if msg == nil || msg.From == nil || !msg.IsCommand() || msg.Command() != "settings" {
		return
	}
	args := strings.Fields(msg.CommandArguments())
	if len(args) < 1 {
		sendText(b.api, msg.Chat.ID, T("en", "settings_usage"))
		return
	}
	groupChatID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || groupChatID == 0 {
		sendText(b.api, msg.Chat.ID, T("en", "settings_usage"))
		return
	}
	if !b.canManageGroupSettings(ctx, groupChatID, msg.From.ID) {
		sendText(b.api, msg.Chat.ID, T("en", "not_group_owner"))
		return
	}
	cfg, err := b.ensureGroup(ctx, groupChatID)
	if err != nil {
		log.Printf("[dashboard] ensure group: %v", err)
		sendText(b.api, msg.Chat.ID, "❌ Could not load this group.")
		return
	}
	b.sendDashboard(ctx, msg.Chat.ID, groupChatID, cfg)
}

func (b *Bot) sendDashboard(ctx context.Context, dmChatID, groupChatID int64, cfg *models.Group) {
	_ = ctx
	if cfg == nil {
		return
	}
	text := dashboardText(groupChatID, cfg)
	msg := tgbotapi.NewMessage(dmChatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = dashboardKeyboard(groupChatID, cfg)
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("[dashboard] send: %v", err)
	}
}

func dashboardText(groupChatID int64, cfg *models.Group) string {
	return fmt.Sprintf("<b>%s</b>\nGroup: <code>%d</code>\nLanguage: <code>%s</code>\nLog channel: <code>%d</code>",
		escapeHTML(T(cfg.Language, "dashboard_title")), groupChatID, escapeHTML(cfg.Language), cfg.LogChannelID)
}

func dashboardKeyboard(groupChatID int64, cfg *models.Group) tgbotapi.InlineKeyboardMarkup {
	toggle := func(label string, val bool, cbKey string) tgbotapi.InlineKeyboardButton {
		icon := "🔴"
		if val {
			icon = "🟢"
		}
		return tgbotapi.NewInlineKeyboardButtonData(icon+" "+label, fmt.Sprintf("dash:%d:%s", groupChatID, cbKey))
	}
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(toggle("CAPTCHA", cfg.CaptchaEnabled, "captcha")),
		tgbotapi.NewInlineKeyboardRow(toggle("Link Filter", cfg.LinksEnabled, "links")),
		tgbotapi.NewInlineKeyboardRow(toggle("Strike Engine", cfg.StrikesEnabled, "strikes")),
		tgbotapi.NewInlineKeyboardRow(toggle("Block Photos", cfg.BlockPhotos, "photos"), toggle("Block Videos", cfg.BlockVideos, "videos")),
		tgbotapi.NewInlineKeyboardRow(toggle("Block Documents", cfg.BlockDocuments, "documents"), toggle("Block Audio", cfg.BlockAudio, "audio")),
		tgbotapi.NewInlineKeyboardRow(toggle("Block Voice", cfg.BlockVoice, "voice"), toggle("Block Stickers", cfg.BlockStickers, "stickers")),
		tgbotapi.NewInlineKeyboardRow(toggle("Block GIFs", cfg.BlockAnimations, "animations"), toggle("Block Video Notes", cfg.BlockVideoNotes, "video_notes")),
		tgbotapi.NewInlineKeyboardRow(toggle("Block Forwards", cfg.BlockForwards, "forwards")),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🇬🇧 English", fmt.Sprintf("dash:%d:lang:en", groupChatID)),
			tgbotapi.NewInlineKeyboardButtonData("🇰🇭 Khmer", fmt.Sprintf("dash:%d:lang:km", groupChatID)),
		),
	)
}

func (b *Bot) handleDashboardCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	if cb == nil || !strings.HasPrefix(cb.Data, "dash:") {
		return
	}
	if cb.Message == nil || cb.From == nil {
		answerCallback(b.api, cb.ID, "Invalid callback.", true)
		return
	}
	parts := strings.Split(cb.Data, ":")
	if len(parts) < 3 || len(parts) > 4 {
		answerCallback(b.api, cb.ID, "Invalid callback data.", true)
		return
	}
	groupChatID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || groupChatID == 0 {
		answerCallback(b.api, cb.ID, "Invalid group id.", true)
		return
	}
	if !b.canManageGroupSettings(ctx, groupChatID, cb.From.ID) {
		answerCallback(b.api, cb.ID, T("en", "not_group_owner"), true)
		return
	}

	cfg, err := b.ensureGroup(ctx, groupChatID)
	if err != nil {
		log.Printf("[dashboard] cb ensure group: %v", err)
		answerCallback(b.api, cb.ID, "Could not load settings.", true)
		return
	}

	updated := *cfg
	action := parts[2]
	switch action {
	case "captcha":
		updated.CaptchaEnabled = !updated.CaptchaEnabled
	case "links":
		updated.LinksEnabled = !updated.LinksEnabled
	case "strikes":
		updated.StrikesEnabled = !updated.StrikesEnabled
	case "photos":
		updated.BlockPhotos = !updated.BlockPhotos
	case "videos":
		updated.BlockVideos = !updated.BlockVideos
	case "documents":
		updated.BlockDocuments = !updated.BlockDocuments
	case "audio":
		updated.BlockAudio = !updated.BlockAudio
	case "voice":
		updated.BlockVoice = !updated.BlockVoice
	case "stickers":
		updated.BlockStickers = !updated.BlockStickers
	case "animations":
		updated.BlockAnimations = !updated.BlockAnimations
	case "video_notes":
		updated.BlockVideoNotes = !updated.BlockVideoNotes
	case "forwards":
		updated.BlockForwards = !updated.BlockForwards
	case "lang":
		if len(parts) != 4 || !supportedLanguage(parts[3]) {
			answerCallback(b.api, cb.ID, "Unsupported language.", true)
			return
		}
		updated.Language = parts[3]
	default:
		answerCallback(b.api, cb.ID, "Unknown setting.", true)
		return
	}

	persistCopy := updated // explicit deep copy; never pass cached pointer to DB persistence.
	persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.store.UpdateGroup(persistCtx, &persistCopy); err != nil {
		log.Printf("[dashboard] persist group %d: %v", groupChatID, err)
		answerCallback(b.api, cb.ID, T(cfg.Language, "dashboard_save_failed"), true)
		return
	}
	b.cache.SetGroup(&persistCopy)

	edit := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, dashboardText(groupChatID, &persistCopy))
	edit.ParseMode = tgbotapi.ModeHTML
	kb := dashboardKeyboard(groupChatID, &persistCopy)
	edit.ReplyMarkup = &kb
	if _, err := b.api.Request(edit); err != nil {
		log.Printf("[dashboard] edit markup: %v", err)
	}
	answerCallback(b.api, cb.ID, T(persistCopy.Language, "dashboard_saved"), false)
}

func (b *Bot) setLogChannel(ctx context.Context, chatID int64, cfg *models.Group, logChannelID int64) {
	updated := *cfg
	updated.LogChannelID = logChannelID
	persistCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := b.store.UpdateGroup(persistCtx, &updated); err != nil {
		log.Printf("[admin] set log channel group=%d log=%d: %v", chatID, logChannelID, err)
		sendText(b.api, chatID, "❌ Could not save log channel.")
		return
	}
	b.cache.SetGroup(&updated)
	if logChannelID == 0 {
		sendText(b.api, chatID, T(updated.Language, "log_channel_cleared"))
		return
	}
	sendText(b.api, chatID, T(updated.Language, "log_channel_set"))
}

func (b *Bot) sendBotPermissionCheck(ctx context.Context, msg *tgbotapi.Message, cfg *models.Group) {
	_ = ctx
	if msg == nil || msg.Chat == nil || cfg == nil {
		return
	}
	cm, err := b.api.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{ChatID: msg.Chat.ID, UserID: b.api.Self.ID},
	})
	if err != nil {
		log.Printf("[checkbot] chat=%d: %v", msg.Chat.ID, err)
		sendText(b.api, msg.Chat.ID, "❌ Could not check bot permissions. Make sure the bot is still in this group/channel.")
		return
	}
	isAdmin := cm.IsCreator() || cm.IsAdministrator()
	status := "❌ Not admin"
	if isAdmin {
		status = "✅ Admin"
	}
	text := fmt.Sprintf(
		"🧪 <b>Bot Permission Check</b>\n"+
			"Bot: <code>@%s</code>\n"+
			"Chat ID: <code>%d</code>\n"+
			"Admin status: <b>%s</b>\n\n"+
			"Required permissions for full moderation:\n"+
			"• Delete messages\n"+
			"• Ban users\n"+
			"• Restrict/mute users\n"+
			"• Change chat permissions for /lock and /unlock\n\n"+
			"If moderation does not work, promote the bot to admin and enable those rights.",
		escapeHTML(b.api.Self.UserName),
		msg.Chat.ID,
		status,
	)
	sendHTML(b.api, msg.Chat.ID, text)
}

func (b *Bot) sendGroupStatus(ctx context.Context, msg *tgbotapi.Message, cfg *models.Group) {
	if msg == nil || cfg == nil {
		return
	}
	stats, err := b.store.GetGroupTodayStats(ctx, cfg.ChatID)
	if err != nil {
		log.Printf("[status] group=%d: %v", cfg.ChatID, err)
		sendText(b.api, msg.Chat.ID, "❌ Could not load group status.")
		return
	}
	text := fmt.Sprintf(
		"📊 <b>Group Status</b>\n"+
			"Chat ID: <code>%d</code>\n"+
			"Language: <code>%s</code>\n"+
			"Log channel: <code>%d</code>\n"+
			"CAPTCHA: <code>%t</code>\n"+
			"Link filter: <code>%t</code>\n"+
			"Strike engine: <code>%t</code>\n"+
			"Block forwards: <code>%t</code>\n"+
			"Blocked media: <code>%s</code>\n\n"+
			"<b>Today</b>\n"+
			"Deleted messages: <code>%d</code>\n"+
			"Strikes issued: <code>%d</code>\n"+
			"Users banned/kicked: <code>%d</code>",
		cfg.ChatID,
		escapeHTML(cfg.Language),
		cfg.LogChannelID,
		cfg.CaptchaEnabled,
		cfg.LinksEnabled,
		cfg.StrikesEnabled,
		cfg.BlockForwards,
		escapeHTML(blockedMediaSummary(cfg)),
		stats.MessagesDeleted,
		stats.StrikesIssued,
		stats.SpammersKicked,
	)
	sendHTML(b.api, msg.Chat.ID, text)
}

func blockedMediaSummary(cfg *models.Group) string {
	if cfg == nil {
		return "none"
	}
	var items []string
	if cfg.BlockPhotos {
		items = append(items, "photos")
	}
	if cfg.BlockVideos {
		items = append(items, "videos")
	}
	if cfg.BlockDocuments {
		items = append(items, "documents")
	}
	if cfg.BlockAudio {
		items = append(items, "audio")
	}
	if cfg.BlockVoice {
		items = append(items, "voice")
	}
	if cfg.BlockStickers {
		items = append(items, "stickers")
	}
	if cfg.BlockAnimations {
		items = append(items, "GIFs")
	}
	if cfg.BlockVideoNotes {
		items = append(items, "video_notes")
	}
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

func (b *Bot) handleBadWordCommand(ctx context.Context, msg *tgbotapi.Message, cfg *models.Group) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) < 1 {
		sendText(b.api, msg.Chat.ID, T(cfg.Language, "badword_usage"))
		return
	}
	sub := strings.ToLower(args[0])
	switch sub {
	case "add":
		word := strings.TrimSpace(strings.Join(args[1:], " "))
		if word == "" {
			sendText(b.api, msg.Chat.ID, T(cfg.Language, "badword_usage"))
			return
		}
		if err := b.store.AddBadWord(ctx, cfg.ChatID, word); err != nil {
			log.Printf("[admin] add badword: %v", err)
			sendText(b.api, msg.Chat.ID, "❌ Could not add bad word.")
			return
		}
		b.cache.DeleteBadWords(cfg.ChatID)
		sendText(b.api, msg.Chat.ID, "✅ Bad word added.")
	case "remove", "delete", "del":
		word := strings.TrimSpace(strings.Join(args[1:], " "))
		if word == "" {
			sendText(b.api, msg.Chat.ID, T(cfg.Language, "badword_usage"))
			return
		}
		if err := b.store.RemoveBadWord(ctx, cfg.ChatID, word); err != nil {
			log.Printf("[admin] remove badword: %v", err)
			sendText(b.api, msg.Chat.ID, "❌ Could not remove bad word.")
			return
		}
		b.cache.DeleteBadWords(cfg.ChatID)
		sendText(b.api, msg.Chat.ID, "✅ Bad word removed.")
	case "list":
		words, err := b.store.GetBadWords(ctx, cfg.ChatID)
		if err != nil {
			log.Printf("[admin] list badwords: %v", err)
			sendText(b.api, msg.Chat.ID, "❌ Could not load bad words.")
			return
		}
		b.cache.SetBadWords(cfg.ChatID, words)
		if len(words) == 0 {
			sendText(b.api, msg.Chat.ID, "No bad words configured.")
			return
		}
		sendHTML(b.api, msg.Chat.ID, "<b>Bad words</b>\n<code>"+escapeHTML(strings.Join(words, "\n"))+"</code>")
	default:
		sendText(b.api, msg.Chat.ID, T(cfg.Language, "badword_usage"))
	}
}

func (b *Bot) handleAllowDomainCommand(ctx context.Context, msg *tgbotapi.Message, cfg *models.Group) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) < 1 {
		sendText(b.api, msg.Chat.ID, T(cfg.Language, "allow_usage"))
		return
	}
	sub := strings.ToLower(args[0])
	switch sub {
	case "add":
		if len(args) < 2 {
			sendText(b.api, msg.Chat.ID, T(cfg.Language, "allow_usage"))
			return
		}
		domain := normalizeDomainForCommand(args[1])
		if domain == "" {
			sendText(b.api, msg.Chat.ID, "❌ Invalid domain.")
			return
		}
		if err := b.store.AddWhitelistDomain(ctx, cfg.ChatID, domain); err != nil {
			log.Printf("[admin] add whitelist: %v", err)
			sendText(b.api, msg.Chat.ID, "❌ Could not add domain.")
			return
		}
		b.cache.DeleteWhitelist(cfg.ChatID)
		sendText(b.api, msg.Chat.ID, "✅ Domain allowed: "+domain)
	case "remove", "delete", "del":
		if len(args) < 2 {
			sendText(b.api, msg.Chat.ID, T(cfg.Language, "allow_usage"))
			return
		}
		domain := normalizeDomainForCommand(args[1])
		if domain == "" {
			sendText(b.api, msg.Chat.ID, "❌ Invalid domain.")
			return
		}
		if err := b.store.RemoveWhitelistDomain(ctx, cfg.ChatID, domain); err != nil {
			log.Printf("[admin] remove whitelist: %v", err)
			sendText(b.api, msg.Chat.ID, "❌ Could not remove domain.")
			return
		}
		b.cache.DeleteWhitelist(cfg.ChatID)
		sendText(b.api, msg.Chat.ID, "✅ Domain removed: "+domain)
	case "list":
		domains, err := b.store.GetWhitelist(ctx, cfg.ChatID)
		if err != nil {
			log.Printf("[admin] list whitelist: %v", err)
			sendText(b.api, msg.Chat.ID, "❌ Could not load domains.")
			return
		}
		b.cache.SetWhitelist(cfg.ChatID, domains)
		if len(domains) == 0 {
			sendText(b.api, msg.Chat.ID, "No domains allowed. Link filter will block all links.")
			return
		}
		sendHTML(b.api, msg.Chat.ID, "<b>Allowed domains</b>\n<code>"+escapeHTML(strings.Join(domains, "\n"))+"</code>")
	default:
		sendText(b.api, msg.Chat.ID, T(cfg.Language, "allow_usage"))
	}
}

func normalizeDomainForCommand(raw string) string {
	hosts := security.ExtractHostnames(raw)
	if len(hosts) > 0 {
		return hosts[0]
	}
	return strings.Trim(strings.ToLower(raw), " .")
}
