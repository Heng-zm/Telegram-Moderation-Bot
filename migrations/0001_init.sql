CREATE TABLE IF NOT EXISTS groups (
    chat_id BIGINT PRIMARY KEY,
    language TEXT NOT NULL DEFAULT 'en' CHECK (language IN ('en', 'km')),
    captcha_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    links_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    strikes_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    block_photos BOOLEAN NOT NULL DEFAULT FALSE,
    block_videos BOOLEAN NOT NULL DEFAULT FALSE,
    block_documents BOOLEAN NOT NULL DEFAULT FALSE,
    block_audio BOOLEAN NOT NULL DEFAULT FALSE,
    block_voice BOOLEAN NOT NULL DEFAULT FALSE,
    block_stickers BOOLEAN NOT NULL DEFAULT FALSE,
    block_animations BOOLEAN NOT NULL DEFAULT FALSE,
    block_video_notes BOOLEAN NOT NULL DEFAULT FALSE,
    block_forwards BOOLEAN NOT NULL DEFAULT FALSE,
    log_channel_id BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Safe upgrade path from older schema that only had block_media.
ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_photos BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_videos BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_documents BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_audio BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_voice BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_stickers BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_animations BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_video_notes BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_forwards BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS log_channel_id BIGINT NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'groups' AND column_name = 'block_media'
    ) THEN
        EXECUTE 'UPDATE groups SET
            block_photos = block_media,
            block_videos = block_media,
            block_documents = block_media,
            block_audio = block_media,
            block_voice = block_media,
            block_stickers = block_media,
            block_animations = block_media,
            block_video_notes = block_media
            WHERE block_media = TRUE';
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS bad_words (
    chat_id BIGINT NOT NULL REFERENCES groups(chat_id) ON DELETE CASCADE,
    word TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chat_id, word)
);

CREATE TABLE IF NOT EXISTS link_whitelist (
    chat_id BIGINT NOT NULL REFERENCES groups(chat_id) ON DELETE CASCADE,
    domain TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chat_id, domain)
);

CREATE TABLE IF NOT EXISTS user_strikes (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL REFERENCES groups(chat_id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL,
    strikes INT NOT NULL DEFAULT 0 CHECK (strikes >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (chat_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_user_strikes_chat_user ON user_strikes(chat_id, user_id);

CREATE TABLE IF NOT EXISTS group_stats (
    chat_id BIGINT NOT NULL REFERENCES groups(chat_id) ON DELETE CASCADE,
    record_date DATE NOT NULL DEFAULT CURRENT_DATE,
    messages_deleted INT NOT NULL DEFAULT 0 CHECK (messages_deleted >= 0),
    spammers_kicked INT NOT NULL DEFAULT 0 CHECK (spammers_kicked >= 0),
    strikes_issued INT NOT NULL DEFAULT 0 CHECK (strikes_issued >= 0),
    PRIMARY KEY (chat_id, record_date)
);

CREATE TABLE IF NOT EXISTS scheduled_tasks (
    id BIGSERIAL PRIMARY KEY,
    task_type TEXT NOT NULL CHECK (task_type IN ('captcha_expire', 'delete_message', 'unmute_user')),
    dedup_key TEXT UNIQUE,
    payload JSONB NOT NULL,
    run_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'done', 'failed', 'cancelled')),
    attempts INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_due ON scheduled_tasks (status, run_at, id);
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_type ON scheduled_tasks (task_type, status);
