package bot

import (
	"context"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type groupRole string

const (
	roleUnknown groupRole = "unknown"
	roleOwner   groupRole = "owner"
	roleAdmin   groupRole = "admin"
	roleMember  groupRole = "member"
)

func (b *Bot) isBotOwner(userID int64) bool {
	if userID == 0 || len(b.botOwners) == 0 {
		return false
	}
	_, ok := b.botOwners[userID]
	return ok
}

// isAdmin keeps the old call sites working, but it now means Telegram
// group/channel moderator rights only. It does NOT include bot-owner rights.
func (b *Bot) isAdmin(ctx context.Context, chatID, userID int64) bool {
	return b.isGroupModerator(ctx, chatID, userID)
}

func (b *Bot) isGroupModerator(ctx context.Context, chatID, userID int64) bool {
	if chatID == 0 || userID == 0 {
		return false
	}
	if allowed, ok := b.adminCache.Get(chatID, userID); ok {
		return allowed
	}
	role, ok := b.resolveGroupRole(ctx, chatID, userID)
	allowed := ok && (role == roleOwner || role == roleAdmin)
	b.adminCache.Set(chatID, userID, allowed)
	return allowed
}

func (b *Bot) isGroupOwner(ctx context.Context, chatID, userID int64) bool {
	role, ok := b.resolveGroupRole(ctx, chatID, userID)
	return ok && role == roleOwner
}

// canManageGroupSettings is intentionally stricter than isAdmin.
// By default only the Telegram creator/owner of that group/channel can change
// group configuration. Bot owners are separate and only bypass this when the
// explicit emergency env flag BOT_OWNER_CAN_MANAGE_GROUPS=true is enabled.
func (b *Bot) canManageGroupSettings(ctx context.Context, chatID, userID int64) bool {
	if chatID == 0 || userID == 0 {
		return false
	}
	if b.opts.BotOwnerCanManageGroups && b.isBotOwner(userID) {
		return true
	}
	return b.isGroupOwner(ctx, chatID, userID)
}

func (b *Bot) resolveGroupRole(ctx context.Context, chatID, userID int64) (groupRole, bool) {
	if chatID == 0 || userID == 0 {
		return roleUnknown, false
	}
	cm, err := b.api.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{ChatID: chatID, UserID: userID},
	})
	if err != nil {
		log.Printf("[role] check chat=%d user=%d: %v", chatID, userID, err)
		return roleUnknown, false
	}
	if cm.IsCreator() {
		return roleOwner, true
	}
	if cm.IsAdministrator() {
		return roleAdmin, true
	}
	return roleMember, true
}
