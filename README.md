# Telemod Telegram Moderation Bot

Production-ready Go Telegram moderation bot with bounded update workers, admin dashboard, CAPTCHA, link filtering, bad-word scanning, strike penalties, audit logs, DB-backed scheduled tasks, anti-flood detection, granular media filters, user reports, and cron-based daily reports.


## Docker build fix

This version includes the required indirect Go modules in `go.mod` so Docker can build with `-mod=readonly`. If you add or remove imports later, run:

```bash
go mod tidy
go test ./...
docker build -t telemod .
```


## Role separation

This build separates bot-level ownership from group/channel ownership.

### Bot Owner / Bot Admin

Set bot owners by Telegram numeric user ID:

```env
BOT_OWNER_IDS=123456789,987654321
BOT_OWNER_CAN_MANAGE_GROUPS=false
```

Bot owners can DM the bot:

- `/admin` or `/botadmin` — open the bot service dashboard.
- `/groups` — list registered groups/channels known by the bot.
- `/tasks` — show scheduled task counts by status.
- `/whoami` — show your Telegram ID and bot-owner status.

By default, bot owners cannot change a customer group/channel configuration unless they are also the Telegram creator/owner of that chat. Set `BOT_OWNER_CAN_MANAGE_GROUPS=true` only when you intentionally want emergency support override.

### Group/Channel Owner

The Telegram creator/owner of a group/channel controls that chat only. Group/channel owner commands:

- `/settings`
- `/lock` / `/unlock`
- `/setlog` / `/clearlog`
- `/badword ...`
- `/allowdomain ...`

Normal Telegram admins can still handle moderation report buttons such as `Ban User` and `Delete & Strike`, but they cannot change bot settings unless they are the Telegram creator/owner.

Use `/whoami` in a group to confirm whether the bot sees you as `owner`, `admin`, or `member`.


## Fix Telegram `getUpdates` conflict

If logs show:

```text
Conflict: terminated by other getUpdates request; make sure that only one bot instance is running
```

it means the same `TELEGRAM_BOT_TOKEN` is being polled by another process. Stop old Render services, local `go run` sessions, Docker containers, or any other bot project using the same token. One Telegram bot token can only have one active long-polling consumer.

This build adds two protections:

- A Postgres advisory polling lock, enabled by default, so duplicate Render instances using the same DB do not poll at the same time.
- Startup webhook cleanup, enabled by default, so old webhooks do not block polling mode.

Config:

```env
BOT_DELETE_WEBHOOK_ON_START=true
BOT_DROP_PENDING_UPDATES_ON_START=false
BOT_DISABLE_DB_POLLING_LOCK=false
```

Keep `BOT_DISABLE_DB_POLLING_LOCK=false` in production. Set `BOT_DROP_PENDING_UPDATES_ON_START=true` only when you intentionally want to clear old pending Telegram updates during a reset.

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

This build also performs an idempotent startup schema check for every required table: `groups`, `bad_words`, `link_whitelist`, `user_strikes`, `group_stats`, and `scheduled_tasks`. If your DB user has `CREATE TABLE`/`ALTER TABLE` permission, missing tables are created automatically. If the DB user has no DDL permission, startup fails clearly instead of logging missing-table errors every few seconds.

If you upgraded from an older ZIP and see `ERROR: relation "scheduled_tasks" does not exist`, run `migrations/0002_scheduled_tasks.sql` or deploy this build with a DB user that can create the table.

## Group/channel owner commands

- `/settings` opens the inline dashboard in the current group. Only the Telegram group/channel owner can change settings by default.
- DM the bot: `/settings <group_chat_id>` to open the same dashboard privately.
- `/lock` and `/unlock` toggle only `CanSendMessages` while preserving current default chat permissions fetched from Telegram.
- `/setlog` sets the current group as the moderation audit log channel.
- `/clearlog` disables audit logging for the group.
- `/badword add <word>`, `/badword remove <word>`, `/badword list`.
- `/allowdomain add <domain>`, `/allowdomain remove <domain>`, `/allowdomain list`.
- `/checkbot` checks whether the bot is admin in the current group/channel and reminds you which Telegram permissions are required.

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

## Health endpoints

- `/livez` returns 200 when the HTTP process is alive. Use this for simple Render port checks.
- `/readyz` returns 200 only when Postgres responds to a ping.
- `/healthz` returns bot, DB pool, queue, and scheduled task count snapshots.

## Notes

When Link Filter is enabled and the whitelist is empty, any detected link is blocked. Add domains with `/allowdomain add example.com`.

## Latest production fixes

- Docker and setup scripts now build `./cmd/telemod` instead of the old non-existent `./cmd/bot` path.
- Hot-path metric writes now use a bounded worker queue (`BOT_METRIC_QUEUE_SIZE`) instead of spawning DB goroutines per violation.
- CAPTCHA task deduplication is now per chat/user, so re-issued CAPTCHA prompts replace stale pending expiry tasks.
- Scheduled task execution now catches panics per task and marks the task failed/retryable instead of killing the worker.
- Non-retryable Telegram errors such as missing messages, kicked bot, or missing rights are treated as completed tasks to prevent an endless retry queue.

## This update round

- Startup now ensures the full core schema, not only `scheduled_tasks`.
- Added `/livez`, `/readyz`, and richer `/healthz` with scheduled task counts.
- Added `/checkbot` for group/channel owners to debug missing Telegram permissions.
- Added bot-owner `/tasks` command and dashboard button.
- Dispatcher now records channel posts and edited messages instead of silently ignoring them.
- Delete metrics are now counted only after a successful Telegram delete request.
