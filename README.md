# Telemod Telegram Moderation Bot

Production-ready Go Telegram moderation bot with bounded update workers, admin dashboard, CAPTCHA, link filtering, bad-word scanning, strike penalties, audit logs, DB-backed scheduled tasks, anti-flood detection, granular media filters, user reports, and cron-based daily reports.


## Docker build fix

This version includes the required indirect Go modules in `go.mod` so Docker can build with `-mod=readonly`. If you add or remove imports later, run:

```bash
go mod tidy
go test ./...
docker build -t telemod .
```

## Run

```bash
cp .env.example .env
# edit TELEGRAM_BOT_TOKEN and DATABASE_URL
go mod tidy
go test ./...
go run ./cmd/telemod
```

For Render, set the start command to:

```bash
go run ./cmd/telemod
```

## Database

Run `migrations/0001_init.sql` once in Supabase/Postgres before starting the bot. It is safe for upgrades from the older schema with `block_media`; the migration copies that old value into the new granular media columns.

## Admin commands in groups

- `/settings` opens the inline dashboard in the current group.
- DM the bot: `/settings <group_chat_id>` to open the same dashboard privately.
- `/lock` and `/unlock` toggle only `CanSendMessages` while preserving current default chat permissions fetched from Telegram.
- `/setlog` sets the current group as the moderation audit log channel.
- `/clearlog` disables audit logging for the group.
- `/badword add <word>`, `/badword remove <word>`, `/badword list`.
- `/allowdomain add <domain>`, `/allowdomain remove <domain>`, `/allowdomain list`.

## User commands

- Reply to a message with `/report` to forward it to `LogChannelID` with admin action buttons:
  - `Ban User`
  - `Delete & Strike`

## Scheduled task queue

Delayed actions are stored in `scheduled_tasks` and processed by a background worker. This replaces unsafe `time.Sleep` goroutines for:

- CAPTCHA expiry and ban.
- Delayed message deletion.
- Mute expiry / unmute.

This lets the bot resume pending moderation actions after a restart.

## Anti-flood

Defaults: more than 5 messages in 3 seconds triggers deletion and a strike. Configure with:

```env
BOT_FLOOD_LIMIT=5
BOT_FLOOD_WINDOW=3s
```

## Daily reports

Daily reports use `github.com/robfig/cron/v3` and UTC by default. Configure with standard 5-field cron syntax:

```env
BOT_DAILY_REPORT_CRON=0 0 * * *
```

## Notes

When Link Filter is enabled and the whitelist is empty, any detected link is blocked. Add domains with `/allowdomain add example.com`.

## Latest production fixes

- Docker and setup scripts now build `./cmd/telemod` instead of the old non-existent `./cmd/bot` path.
- Hot-path metric writes now use a bounded worker queue (`BOT_METRIC_QUEUE_SIZE`) instead of spawning DB goroutines per violation.
- CAPTCHA task deduplication is now per chat/user, so re-issued CAPTCHA prompts replace stale pending expiry tasks.
- Scheduled task execution now catches panics per task and marks the task failed/retryable instead of killing the worker.
- Non-retryable Telegram errors such as missing messages, kicked bot, or missing rights are treated as completed tasks to prevent an endless retry queue.
