-- ============================================================
-- TeleMod · Supabase PostgreSQL Schema
-- Run this file once against your Supabase project SQL editor
-- or via psql: psql "$DATABASE_URL" -f migrations/001_init.sql
-- ============================================================

-- ── Core Tenant Table ────────────────────────────────────────
CREATE TABLE IF NOT EXISTS groups (
    chat_id         BIGINT PRIMARY KEY,
    language        VARCHAR(5)  NOT NULL DEFAULT 'en',
    captcha_enabled BOOLEAN     NOT NULL DEFAULT TRUE,
    links_enabled   BOOLEAN     NOT NULL DEFAULT TRUE,
    strikes_enabled BOOLEAN     NOT NULL DEFAULT TRUE,
    block_media     BOOLEAN     NOT NULL DEFAULT FALSE,
    block_forwards  BOOLEAN     NOT NULL DEFAULT FALSE,
    log_channel_id  BIGINT      NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── Prohibited Word Matrix ────────────────────────────────────
CREATE TABLE IF NOT EXISTS bad_words (
    id      SERIAL  PRIMARY KEY,
    chat_id BIGINT  NOT NULL REFERENCES groups(chat_id) ON DELETE CASCADE,
    word    VARCHAR(255) NOT NULL,
    UNIQUE (chat_id, word)
);

-- ── Link Whitelist ────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS link_whitelist (
    id      SERIAL  PRIMARY KEY,
    chat_id BIGINT  NOT NULL REFERENCES groups(chat_id) ON DELETE CASCADE,
    domain  VARCHAR(255) NOT NULL,
    UNIQUE (chat_id, domain)
);

-- ── Strike Ledger ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS user_strikes (
    id         SERIAL  PRIMARY KEY,
    chat_id    BIGINT  NOT NULL REFERENCES groups(chat_id) ON DELETE CASCADE,
    user_id    BIGINT  NOT NULL,
    strikes    INT     NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (chat_id, user_id)
);

-- ── Daily Stats ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS group_stats (
    chat_id          BIGINT NOT NULL REFERENCES groups(chat_id) ON DELETE CASCADE,
    record_date      DATE   NOT NULL DEFAULT CURRENT_DATE,
    messages_deleted INT    NOT NULL DEFAULT 0,
    spammers_kicked  INT    NOT NULL DEFAULT 0,
    strikes_issued   INT    NOT NULL DEFAULT 0,
    PRIMARY KEY (chat_id, record_date)
);

-- ── Performance Indexes ───────────────────────────────────────
CREATE INDEX IF NOT EXISTS idx_user_strikes_lookup  ON user_strikes(chat_id, user_id);
CREATE INDEX IF NOT EXISTS idx_group_stats_date     ON group_stats(record_date);
CREATE INDEX IF NOT EXISTS idx_link_whitelist_lookup ON link_whitelist(chat_id);
CREATE INDEX IF NOT EXISTS idx_bad_words_lookup     ON bad_words(chat_id);
