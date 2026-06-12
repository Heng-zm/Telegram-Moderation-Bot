# TeleMod — Enterprise Multi-Tenant Telegram Moderation Bot

A high-performance Go bot for moderating multiple Telegram groups simultaneously, backed by Supabase (PostgreSQL). Supports English and Khmer, with near-zero message latency via async goroutines and sync.Map caching.

---

## Features

| Module | Description |
|---|---|
| **Linguistic Scanner** | Fuzzy Levenshtein matching for English; direct substring scan for Khmer |
| **CAPTCHA Gatekeeper** | Mute on join → inline button → 60 s async expiry timer |
| **Strike Engine** | 3-tier atomic penalties: warning → 2 h mute → permanent ban |
| **Link Whitelist** | Hostname extraction with exact & subdomain matching |
| **Media/Forward Filter** | Block photos, voice, stickers, animations, forwards per group |
| **Emergency Lock** | `/lock` / `/unlock` instantly toggle chat-wide write permissions |
| **DM Dashboard** | Interactive 🟢/🔴 toggle panel for admins via private chat |
| **Audit Logging** | Async channel-based routing to a configurable log channel |
| **Daily Stats Cron** | 24-hour ticker aggregates and reports per-group metrics |

---

## Quick Start

### 1 · Prerequisites

- Go 1.22+
- A Telegram bot token from [@BotFather](https://t.me/botfather)
- A [Supabase](https://supabase.com) project (free tier works)

### 2 · Database Setup

Run the migration against your Supabase project:

```bash
psql "$DATABASE_URL" -f migrations/001_init.sql
```

Or paste the contents into the Supabase SQL editor.

### 3 · Configuration

```bash
cp .env.example .env
# Edit .env and fill in TELEGRAM_BOT_TOKEN and DATABASE_URL
```

### 4 · Run Locally

```bash
go run ./cmd/bot
```

### 5 · Docker

```bash
docker build -t telemod .
docker run --env-file .env telemod
```

---

## Admin Commands

| Command | Where | Effect |
|---|---|---|
| `/lock` | Group (admin) | Disables writing for all members |
| `/unlock` | Group (admin) | Restores writing permissions |
| `/settings <chat_id>` | Private DM | Opens interactive settings dashboard |

---

## Architecture

```
[Telegram Update]
      │
      ├─ New Member ──► Mute ──► CAPTCHA prompt ──► 60s goroutine timer
      │
      └─ Message
            │
            ├─ Admin + Command ──► /lock / /unlock
            │
            └─ User Message
                  │
                  ├─ [Step 1] Forward/Media block check
                  ├─ [Step 2] Link extraction vs whitelist
                  └─ [Step 3] Linguistic scanner (EN fuzzy / KM substring)
                                    │
                     ┌─────────────┴────────────┐
                     ▼                           ▼
               [Passes]                  [Violation]
                                          │
                                    DeleteMessage
                                    go trackMetric()
                                    go routeAuditLog()
                                    Strike engine (atomic DB upsert)
```

### Concurrency Model

- Each Telegram update runs in its own goroutine — no update ever blocks another.
- `sync.Map` provides lock-free reads on the hot path for group configs, bad words, and whitelists.
- `sync.RWMutex` guards in-place config mutations during dashboard callbacks.
- Metrics, audit logs, and CAPTCHA timers all run in separate goroutines, keeping the main message loop latency near zero.

---

## Running Tests

```bash
go test ./internal/security/...
```

---

## Environment Variables

| Variable | Description |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Bot token from @BotFather |
| `DATABASE_URL` | Supabase PostgreSQL connection string |
