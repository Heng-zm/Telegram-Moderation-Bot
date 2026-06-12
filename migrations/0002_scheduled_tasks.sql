-- Creates the restart-safe scheduled task queue used for CAPTCHA expiry,
-- delayed message deletion, and mute/unmute recovery.
-- Safe to run more than once.

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
