package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telemod/internal/models"
)

func (b *Bot) scheduledTaskWorker(ctx context.Context) {
	recoverCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	if recovered, err := b.store.RecoverRunningTasks(recoverCtx); err != nil {
		log.Printf("[tasks] recover running tasks: %v", err)
	} else if recovered > 0 {
		log.Printf("[tasks] recovered %d running task(s) after restart", recovered)
	}
	cancel()

	b.processDueTasks(ctx)

	ticker := time.NewTicker(b.opts.TaskPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.processDueTasks(ctx)
		}
	}
}

func (b *Bot) processDueTasks(ctx context.Context) {
	tasks, err := b.store.ClaimDueTasks(ctx, b.opts.TaskBatchSize)
	if err != nil {
		log.Printf("[tasks] claim due tasks: %v", err)
		return
	}
	for _, task := range tasks {
		if task == nil {
			continue
		}
		jobCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := b.executeScheduledTask(jobCtx, task)
		cancel()

		persistCtx, persistCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err != nil {
			log.Printf("[tasks] execute id=%d type=%s attempts=%d: %v", task.ID, task.Type, task.Attempts, err)
			if failErr := b.store.FailTask(persistCtx, task.ID, task.Attempts, err.Error(), retryAfter(task.Attempts), b.opts.TaskMaxAttempts); failErr != nil {
				log.Printf("[tasks] fail task id=%d: %v", task.ID, failErr)
			}
		} else if completeErr := b.store.CompleteTask(persistCtx, task.ID); completeErr != nil {
			log.Printf("[tasks] complete task id=%d: %v", task.ID, completeErr)
		}
		persistCancel()
	}
}

func (b *Bot) executeScheduledTask(ctx context.Context, task *models.ScheduledTask) error {
	switch task.Type {
	case models.TaskCaptchaExpire:
		var payload models.CaptchaExpirePayload
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			return fmt.Errorf("decode captcha payload: %w", err)
		}
		return b.executeCaptchaExpireTask(ctx, payload)
	case models.TaskDeleteMessage:
		var payload models.DeleteMessagePayload
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			return fmt.Errorf("decode delete payload: %w", err)
		}
		return b.executeDeleteMessageTask(ctx, payload)
	case models.TaskUnmuteUser:
		var payload models.UnmuteUserPayload
		if err := json.Unmarshal(task.Payload, &payload); err != nil {
			return fmt.Errorf("decode unmute payload: %w", err)
		}
		return b.executeUnmuteUserTask(ctx, payload)
	default:
		return fmt.Errorf("unknown scheduled task type %q", task.Type)
	}
}

func (b *Bot) executeCaptchaExpireTask(ctx context.Context, payload models.CaptchaExpirePayload) error {
	b.captchas.Delete(payload.ChatID, payload.UserID)

	if payload.MessageID != 0 {
		if _, err := b.api.Request(tgbotapi.NewDeleteMessage(payload.ChatID, payload.MessageID)); err != nil {
			log.Printf("[captcha] delete expired prompt chat=%d msg=%d: %v", payload.ChatID, payload.MessageID, err)
		}
	}

	ban := tgbotapi.BanChatMemberConfig{ChatMemberConfig: tgbotapi.ChatMemberConfig{ChatID: payload.ChatID, UserID: payload.UserID}}
	if _, err := b.api.Request(ban); err != nil {
		return fmt.Errorf("ban expired captcha user=%d chat=%d: %w", payload.UserID, payload.ChatID, err)
	}

	cfg := b.cache.GetGroup(payload.ChatID)
	if cfg == nil {
		loaded, err := b.store.GetGroup(ctx, payload.ChatID)
		if err == nil {
			cfg = loaded
			b.cache.SetGroup(loaded)
		}
	}
	lang := "en"
	if cfg != nil && cfg.Language != "" {
		lang = cfg.Language
	}
	note, err := b.api.Send(tgbotapi.NewMessage(payload.ChatID, T(lang, "captcha_expired")))
	if err != nil {
		return fmt.Errorf("send captcha expiry notice: %w", err)
	}
	b.enqueueDeleteMessage(ctx, payload.ChatID, note.MessageID, time.Now().Add(b.opts.CaptchaNoticeTTL))
	return nil
}

func (b *Bot) executeDeleteMessageTask(ctx context.Context, payload models.DeleteMessagePayload) error {
	_ = ctx
	if payload.ChatID == 0 || payload.MessageID == 0 {
		return nil
	}
	if _, err := b.api.Request(tgbotapi.NewDeleteMessage(payload.ChatID, payload.MessageID)); err != nil {
		return fmt.Errorf("delete message chat=%d msg=%d: %w", payload.ChatID, payload.MessageID, err)
	}
	return nil
}

func (b *Bot) executeUnmuteUserTask(ctx context.Context, payload models.UnmuteUserPayload) error {
	_ = ctx
	if payload.ChatID == 0 || payload.UserID == 0 {
		return nil
	}
	restore := tgbotapi.RestrictChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{ChatID: payload.ChatID, UserID: payload.UserID},
		Permissions: &tgbotapi.ChatPermissions{
			CanSendMessages:       true,
			CanSendMediaMessages:  true,
			CanSendOtherMessages:  true,
			CanAddWebPagePreviews: true,
		},
	}
	if _, err := b.api.Request(restore); err != nil {
		return fmt.Errorf("unmute user=%d chat=%d: %w", payload.UserID, payload.ChatID, err)
	}
	return nil
}

func (b *Bot) enqueueCaptchaExpiry(ctx context.Context, chatID, userID int64, messageID int, expiresAt time.Time) {
	payload := models.CaptchaExpirePayload{ChatID: chatID, UserID: userID, MessageID: messageID}
	if _, err := b.store.EnqueueTask(ctx, models.TaskCaptchaExpire, captchaTaskKey(chatID, userID, messageID), payload, expiresAt); err != nil {
		log.Printf("[tasks] enqueue captcha expiry chat=%d user=%d msg=%d: %v", chatID, userID, messageID, err)
	}
}

func (b *Bot) enqueueDeleteMessage(ctx context.Context, chatID int64, messageID int, runAt time.Time) {
	if chatID == 0 || messageID == 0 {
		return
	}
	payload := models.DeleteMessagePayload{ChatID: chatID, MessageID: messageID}
	if _, err := b.store.EnqueueTask(ctx, models.TaskDeleteMessage, deleteTaskKey(chatID, messageID), payload, runAt); err != nil {
		log.Printf("[tasks] enqueue delete message chat=%d msg=%d: %v", chatID, messageID, err)
	}
}

func (b *Bot) enqueueUnmuteUser(ctx context.Context, chatID, userID int64, runAt time.Time) {
	if chatID == 0 || userID == 0 {
		return
	}
	payload := models.UnmuteUserPayload{ChatID: chatID, UserID: userID}
	if _, err := b.store.EnqueueTask(ctx, models.TaskUnmuteUser, unmuteTaskKey(chatID, userID), payload, runAt); err != nil {
		log.Printf("[tasks] enqueue unmute chat=%d user=%d: %v", chatID, userID, err)
	}
}

func (b *Bot) cancelCaptchaExpiry(ctx context.Context, chatID, userID int64, messageID int) bool {
	ok, err := b.store.CancelTaskByDedupKey(ctx, captchaTaskKey(chatID, userID, messageID))
	if err != nil {
		log.Printf("[tasks] cancel captcha expiry chat=%d user=%d msg=%d: %v", chatID, userID, messageID, err)
	}
	return ok
}

func captchaTaskKey(chatID, userID int64, messageID int) string {
	return fmt.Sprintf("captcha:%d:%d:%d", chatID, userID, messageID)
}

func deleteTaskKey(chatID int64, messageID int) string {
	return fmt.Sprintf("delete:%d:%d", chatID, messageID)
}

func unmuteTaskKey(chatID, userID int64) string {
	return fmt.Sprintf("unmute:%d:%d", chatID, userID)
}

func retryAfter(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	seconds := attempts * attempts * 5
	if seconds > 300 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}
