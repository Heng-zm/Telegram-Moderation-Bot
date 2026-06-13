package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"telemod/internal/models"
)

type Options struct {
	MaxConns int
	MinConns int
}

type Store struct {
	pool *pgxpool.Pool
}

// AdvisoryLock is a process-level PostgreSQL advisory lock held on a single
// dedicated connection. The connection must stay open for the lock lifetime.
type AdvisoryLock struct {
	conn *pgxpool.Conn
	key  int64
	once sync.Once
}

// TryAcquireAdvisoryLock attempts to acquire a session-level advisory lock.
// The returned lock must be released by the caller. If acquired is false,
// another process using the same database already owns the lock.
func (s *Store) TryAcquireAdvisoryLock(ctx context.Context, key int64) (*AdvisoryLock, bool, error) {
	if s == nil || s.pool == nil {
		return nil, false, fmt.Errorf("db: advisory lock: store is closed")
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("db: acquire advisory lock connection: %w", err)
	}

	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&acquired); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("db: acquire advisory lock %d: %w", key, err)
	}
	if !acquired {
		conn.Release()
		return nil, false, nil
	}
	return &AdvisoryLock{conn: conn, key: key}, true, nil
}

// Release unlocks and releases the dedicated advisory-lock connection.
func (l *AdvisoryLock) Release(ctx context.Context) error {
	if l == nil {
		return nil
	}
	var releaseErr error
	l.once.Do(func() {
		if l.conn == nil {
			return
		}
		defer l.conn.Release()

		var unlocked bool
		if err := l.conn.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, l.key).Scan(&unlocked); err != nil {
			releaseErr = fmt.Errorf("db: release advisory lock %d: %w", l.key, err)
			return
		}
		if !unlocked {
			releaseErr = fmt.Errorf("db: advisory lock %d was not held by this session", l.key)
		}
	})
	return releaseErr
}

func New(ctx context.Context, dsn string, opts Options) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("db: DATABASE_URL is empty")
	}
	if opts.MaxConns <= 0 {
		opts.MaxConns = 20
	}
	if opts.MinConns < 0 {
		opts.MinConns = 0
	}
	if opts.MinConns > opts.MaxConns {
		opts.MinConns = opts.MaxConns
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: parse config: %w", err)
	}
	cfg.MaxConns = int32(opts.MaxConns)
	cfg.MinConns = int32(opts.MinConns)
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: connect pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}

	store := &Store{pool: pool}
	if err := store.EnsureCoreSchema(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

// EnsureCoreSchema creates/updates every table the bot needs. It is
// idempotent and safe for old deployments that missed one or more migration
// files. If the DB user has no DDL permission, startup fails clearly instead of
// running with missing tables and spamming runtime errors.
func (s *Store) EnsureCoreSchema(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("db: ensure schema: store is closed")
	}

	statements := []string{
		`CREATE TABLE IF NOT EXISTS groups (
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
		)`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS language TEXT NOT NULL DEFAULT 'en'`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS captcha_enabled BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS links_enabled BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS strikes_enabled BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_photos BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_videos BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_documents BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_audio BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_voice BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_stickers BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_animations BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_video_notes BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS block_forwards BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS log_channel_id BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`,
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = current_schema() AND table_name = 'groups' AND column_name = 'block_media'
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
		END $$`,
		`CREATE TABLE IF NOT EXISTS bad_words (
			chat_id BIGINT NOT NULL REFERENCES groups(chat_id) ON DELETE CASCADE,
			word TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (chat_id, word)
		)`,
		`CREATE TABLE IF NOT EXISTS link_whitelist (
			chat_id BIGINT NOT NULL REFERENCES groups(chat_id) ON DELETE CASCADE,
			domain TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (chat_id, domain)
		)`,
		`CREATE TABLE IF NOT EXISTS user_strikes (
			id BIGSERIAL PRIMARY KEY,
			chat_id BIGINT NOT NULL REFERENCES groups(chat_id) ON DELETE CASCADE,
			user_id BIGINT NOT NULL,
			strikes INT NOT NULL DEFAULT 0 CHECK (strikes >= 0),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (chat_id, user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_strikes_chat_user ON user_strikes(chat_id, user_id)`,
		`CREATE TABLE IF NOT EXISTS group_stats (
			chat_id BIGINT NOT NULL REFERENCES groups(chat_id) ON DELETE CASCADE,
			record_date DATE NOT NULL DEFAULT CURRENT_DATE,
			messages_deleted INT NOT NULL DEFAULT 0 CHECK (messages_deleted >= 0),
			spammers_kicked INT NOT NULL DEFAULT 0 CHECK (spammers_kicked >= 0),
			strikes_issued INT NOT NULL DEFAULT 0 CHECK (strikes_issued >= 0),
			PRIMARY KEY (chat_id, record_date)
		)`,
		`CREATE TABLE IF NOT EXISTS scheduled_tasks (
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
		)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_due ON scheduled_tasks (status, run_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_type ON scheduled_tasks (task_type, status)`,
	}

	for _, q := range statements {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("db: ensure core schema: %w", err)
		}
	}
	return nil
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("db: ping: store is closed")
	}
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("db: ping: %w", err)
	}
	return nil
}

func (s *Store) ScheduledTaskCounts(ctx context.Context) (map[string]int64, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("db: task counts: store is closed")
	}
	const q = `SELECT status, COUNT(*) FROM scheduled_tasks GROUP BY status`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("db: scheduled task counts: %w", err)
	}
	defer rows.Close()
	out := map[string]int64{
		"pending":   0,
		"running":   0,
		"done":      0,
		"failed":    0,
		"cancelled": 0,
	}
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("db: scan scheduled task count: %w", err)
		}
		out[status] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: rows scheduled task count: %w", err)
	}
	return out, nil
}

func (s *Store) HealthSnapshot() map[string]any {
	if s == nil || s.pool == nil {
		return map[string]any{"status": "closed"}
	}
	st := s.pool.Stat()
	return map[string]any{
		"status":                 "ok",
		"acquired_conns":         st.AcquiredConns(),
		"idle_conns":             st.IdleConns(),
		"total_conns":            st.TotalConns(),
		"max_conns":              st.MaxConns(),
		"acquire_count":          st.AcquireCount(),
		"canceled_acquire_count": st.CanceledAcquireCount(),
	}
}

func (s *Store) GetOrCreateGroup(ctx context.Context, chatID int64) (*models.Group, error) {
	const q = `
		INSERT INTO groups (chat_id)
		VALUES ($1)
		ON CONFLICT (chat_id) DO NOTHING`
	if _, err := s.pool.Exec(ctx, q, chatID); err != nil {
		return nil, fmt.Errorf("db: insert group %d: %w", chatID, err)
	}
	return s.GetGroup(ctx, chatID)
}

func (s *Store) GetGroup(ctx context.Context, chatID int64) (*models.Group, error) {
	const q = `
		SELECT chat_id,
		       language,
		       captcha_enabled,
		       links_enabled,
		       strikes_enabled,
		       block_photos,
		       block_videos,
		       block_documents,
		       block_audio,
		       block_voice,
		       block_stickers,
		       block_animations,
		       block_video_notes,
		       block_forwards,
		       log_channel_id,
		       created_at
		FROM groups
		WHERE chat_id = $1`

	g := &models.Group{}
	err := s.pool.QueryRow(ctx, q, chatID).Scan(
		&g.ChatID,
		&g.Language,
		&g.CaptchaEnabled,
		&g.LinksEnabled,
		&g.StrikesEnabled,
		&g.BlockPhotos,
		&g.BlockVideos,
		&g.BlockDocuments,
		&g.BlockAudio,
		&g.BlockVoice,
		&g.BlockStickers,
		&g.BlockAnimations,
		&g.BlockVideoNotes,
		&g.BlockForwards,
		&g.LogChannelID,
		&g.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("db: get group %d: %w", chatID, err)
	}
	return g, nil
}

func (s *Store) UpdateGroup(ctx context.Context, g *models.Group) error {
	if g == nil {
		return fmt.Errorf("db: update group: nil group")
	}
	if !isSupportedLanguage(g.Language) {
		return fmt.Errorf("db: update group %d: unsupported language %q", g.ChatID, g.Language)
	}
	const q = `
		UPDATE groups SET
			language          = $2,
			captcha_enabled   = $3,
			links_enabled     = $4,
			strikes_enabled   = $5,
			block_photos      = $6,
			block_videos      = $7,
			block_documents   = $8,
			block_audio       = $9,
			block_voice       = $10,
			block_stickers    = $11,
			block_animations  = $12,
			block_video_notes = $13,
			block_forwards    = $14,
			log_channel_id    = $15
		WHERE chat_id = $1`
	cmd, err := s.pool.Exec(ctx, q,
		g.ChatID,
		g.Language,
		g.CaptchaEnabled,
		g.LinksEnabled,
		g.StrikesEnabled,
		g.BlockPhotos,
		g.BlockVideos,
		g.BlockDocuments,
		g.BlockAudio,
		g.BlockVoice,
		g.BlockStickers,
		g.BlockAnimations,
		g.BlockVideoNotes,
		g.BlockForwards,
		g.LogChannelID,
	)
	if err != nil {
		return fmt.Errorf("db: update group %d: %w", g.ChatID, err)
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("db: update group %d: no rows affected", g.ChatID)
	}
	return nil
}

func (s *Store) SetLogChannel(ctx context.Context, chatID, logChannelID int64) (*models.Group, error) {
	g, err := s.GetOrCreateGroup(ctx, chatID)
	if err != nil {
		return nil, err
	}
	g.LogChannelID = logChannelID
	if err := s.UpdateGroup(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}

func (s *Store) GetBadWords(ctx context.Context, chatID int64) ([]string, error) {
	const q = `SELECT word FROM bad_words WHERE chat_id = $1 ORDER BY word ASC`
	rows, err := s.pool.Query(ctx, q, chatID)
	if err != nil {
		return nil, fmt.Errorf("db: get bad words %d: %w", chatID, err)
	}
	defer rows.Close()
	words, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("db: collect bad words %d: %w", chatID, err)
	}
	return words, nil
}

func (s *Store) AddBadWord(ctx context.Context, chatID int64, word string) error {
	word = strings.TrimSpace(word)
	if word == "" {
		return fmt.Errorf("db: bad word is empty")
	}
	const q = `INSERT INTO bad_words (chat_id, word) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err := s.pool.Exec(ctx, q, chatID, word)
	if err != nil {
		return fmt.Errorf("db: add bad word %d: %w", chatID, err)
	}
	return nil
}

func (s *Store) RemoveBadWord(ctx context.Context, chatID int64, word string) error {
	word = strings.TrimSpace(word)
	if word == "" {
		return fmt.Errorf("db: bad word is empty")
	}
	const q = `DELETE FROM bad_words WHERE chat_id = $1 AND word = $2`
	_, err := s.pool.Exec(ctx, q, chatID, word)
	if err != nil {
		return fmt.Errorf("db: remove bad word %d: %w", chatID, err)
	}
	return nil
}

func (s *Store) GetWhitelist(ctx context.Context, chatID int64) ([]string, error) {
	const q = `SELECT domain FROM link_whitelist WHERE chat_id = $1 ORDER BY domain ASC`
	rows, err := s.pool.Query(ctx, q, chatID)
	if err != nil {
		return nil, fmt.Errorf("db: get whitelist %d: %w", chatID, err)
	}
	defer rows.Close()
	domains, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("db: collect whitelist %d: %w", chatID, err)
	}
	return domains, nil
}

func (s *Store) AddWhitelistDomain(ctx context.Context, chatID int64, domain string) error {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return fmt.Errorf("db: domain is empty")
	}
	const q = `INSERT INTO link_whitelist (chat_id, domain) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err := s.pool.Exec(ctx, q, chatID, domain)
	if err != nil {
		return fmt.Errorf("db: add whitelist domain %d: %w", chatID, err)
	}
	return nil
}

func (s *Store) RemoveWhitelistDomain(ctx context.Context, chatID int64, domain string) error {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return fmt.Errorf("db: domain is empty")
	}
	const q = `DELETE FROM link_whitelist WHERE chat_id = $1 AND domain = $2`
	_, err := s.pool.Exec(ctx, q, chatID, domain)
	if err != nil {
		return fmt.Errorf("db: remove whitelist domain %d: %w", chatID, err)
	}
	return nil
}

func (s *Store) IncrementStrike(ctx context.Context, chatID, userID int64) (int, error) {
	const q = `
		INSERT INTO user_strikes (chat_id, user_id, strikes, updated_at)
		VALUES ($1, $2, 1, NOW())
		ON CONFLICT (chat_id, user_id) DO UPDATE
			SET strikes = user_strikes.strikes + 1,
			    updated_at = NOW()
		RETURNING strikes`

	var total int
	if err := s.pool.QueryRow(ctx, q, chatID, userID).Scan(&total); err != nil {
		return 0, fmt.Errorf("db: increment strike chat=%d user=%d: %w", chatID, userID, err)
	}
	return total, nil
}

func (s *Store) DeleteUserStrike(ctx context.Context, chatID, userID int64) error {
	const q = `DELETE FROM user_strikes WHERE chat_id = $1 AND user_id = $2`
	_, err := s.pool.Exec(ctx, q, chatID, userID)
	if err != nil {
		return fmt.Errorf("db: delete user strike chat=%d user=%d: %w", chatID, userID, err)
	}
	return nil
}

func (s *Store) TrackMetric(ctx context.Context, chatID int64, column string) error {
	if !isMetricColumn(column) {
		return fmt.Errorf("db: invalid metric column %q", column)
	}
	q := fmt.Sprintf(`
		INSERT INTO group_stats (chat_id, record_date, %s)
		VALUES ($1, CURRENT_DATE, 1)
		ON CONFLICT (chat_id, record_date) DO UPDATE
			SET %s = group_stats.%s + 1`, column, column, column)
	_, err := s.pool.Exec(ctx, q, chatID)
	if err != nil {
		return fmt.Errorf("db: track metric %s for %d: %w", column, chatID, err)
	}
	return nil
}

func (s *Store) GetDailyStats(ctx context.Context) ([]*models.GroupStats, error) {
	const q = `
		SELECT gs.chat_id,
		       gs.record_date,
		       gs.messages_deleted,
		       gs.spammers_kicked,
		       gs.strikes_issued,
		       COALESCE(g.language, 'en') AS language,
		       COALESCE(g.log_channel_id, 0) AS log_channel_id
		FROM group_stats gs
		LEFT JOIN groups g ON g.chat_id = gs.chat_id
		WHERE gs.record_date = CURRENT_DATE
		ORDER BY gs.chat_id ASC`

	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("db: get daily stats: %w", err)
	}
	defer rows.Close()

	var out []*models.GroupStats
	for rows.Next() {
		gs := &models.GroupStats{}
		if err := rows.Scan(
			&gs.ChatID,
			&gs.RecordDate,
			&gs.MessagesDeleted,
			&gs.SpammersKicked,
			&gs.StrikesIssued,
			&gs.Language,
			&gs.LogChannelID,
		); err != nil {
			return nil, fmt.Errorf("db: scan daily stats: %w", err)
		}
		out = append(out, gs)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: rows daily stats: %w", err)
	}
	return out, nil
}

func (s *Store) GetAllGroups(ctx context.Context) ([]*models.Group, error) {
	const q = `
		SELECT chat_id,
		       language,
		       captcha_enabled,
		       links_enabled,
		       strikes_enabled,
		       block_photos,
		       block_videos,
		       block_documents,
		       block_audio,
		       block_voice,
		       block_stickers,
		       block_animations,
		       block_video_notes,
		       block_forwards,
		       log_channel_id,
		       created_at
		FROM groups
		ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("db: get all groups: %w", err)
	}
	defer rows.Close()

	var out []*models.Group
	for rows.Next() {
		g := &models.Group{}
		if err := rows.Scan(
			&g.ChatID,
			&g.Language,
			&g.CaptchaEnabled,
			&g.LinksEnabled,
			&g.StrikesEnabled,
			&g.BlockPhotos,
			&g.BlockVideos,
			&g.BlockDocuments,
			&g.BlockAudio,
			&g.BlockVoice,
			&g.BlockStickers,
			&g.BlockAnimations,
			&g.BlockVideoNotes,
			&g.BlockForwards,
			&g.LogChannelID,
			&g.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("db: scan group: %w", err)
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: rows all groups: %w", err)
	}
	return out, nil
}

func (s *Store) EnqueueTask(ctx context.Context, taskType models.ScheduledTaskType, dedupKey string, payload any, runAt time.Time) (int64, error) {
	if !isSupportedTaskType(taskType) {
		return 0, fmt.Errorf("db: unsupported scheduled task type %q", taskType)
	}
	if runAt.IsZero() {
		runAt = time.Now()
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("db: marshal task payload %s: %w", taskType, err)
	}
	if strings.TrimSpace(dedupKey) != "" {
		const q = `
			INSERT INTO scheduled_tasks (task_type, dedup_key, payload, run_at, status, attempts, last_error)
			VALUES ($1, $2, $3, $4, 'pending', 0, '')
			ON CONFLICT (dedup_key) DO UPDATE SET
				task_type = EXCLUDED.task_type,
				payload = EXCLUDED.payload,
				run_at = EXCLUDED.run_at,
				status = 'pending',
				attempts = 0,
				last_error = '',
				updated_at = NOW()
			RETURNING id`
		var id int64
		if err := s.pool.QueryRow(ctx, q, taskType, dedupKey, raw, runAt).Scan(&id); err != nil {
			return 0, fmt.Errorf("db: enqueue task %s key=%s: %w", taskType, dedupKey, err)
		}
		return id, nil
	}

	const q = `
		INSERT INTO scheduled_tasks (task_type, payload, run_at, status, attempts, last_error)
		VALUES ($1, $2, $3, 'pending', 0, '')
		RETURNING id`
	var id int64
	if err := s.pool.QueryRow(ctx, q, taskType, raw, runAt).Scan(&id); err != nil {
		return 0, fmt.Errorf("db: enqueue task %s: %w", taskType, err)
	}
	return id, nil
}

func (s *Store) CancelTaskByDedupKey(ctx context.Context, dedupKey string) (bool, error) {
	if strings.TrimSpace(dedupKey) == "" {
		return false, nil
	}
	const q = `
		UPDATE scheduled_tasks
		SET status = 'cancelled', updated_at = NOW()
		WHERE dedup_key = $1 AND status = 'pending'`
	cmd, err := s.pool.Exec(ctx, q, dedupKey)
	if err != nil {
		return false, fmt.Errorf("db: cancel task key=%s: %w", dedupKey, err)
	}
	return cmd.RowsAffected() > 0, nil
}

func (s *Store) ClaimDueTasks(ctx context.Context, limit int) ([]*models.ScheduledTask, error) {
	if limit <= 0 {
		limit = 25
	}
	const q = `
		WITH due AS (
			SELECT id
			FROM scheduled_tasks
			WHERE status = 'pending' AND run_at <= NOW()
			ORDER BY run_at ASC, id ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE scheduled_tasks AS t
		SET status = 'running', attempts = attempts + 1, updated_at = NOW()
		FROM due
		WHERE t.id = due.id
		RETURNING t.id, t.task_type, COALESCE(t.dedup_key, ''), t.payload, t.run_at,
		          t.status, t.attempts, COALESCE(t.last_error, ''), t.created_at, t.updated_at`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("db: claim due tasks: %w", err)
	}
	defer rows.Close()

	var out []*models.ScheduledTask
	for rows.Next() {
		t := &models.ScheduledTask{}
		if err := rows.Scan(&t.ID, &t.Type, &t.DedupKey, &t.Payload, &t.RunAt, &t.Status, &t.Attempts, &t.LastError, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("db: scan scheduled task: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: rows scheduled tasks: %w", err)
	}
	return out, nil
}

func (s *Store) CompleteTask(ctx context.Context, id int64) error {
	const q = `UPDATE scheduled_tasks SET status = 'done', updated_at = NOW() WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("db: complete task %d: %w", id, err)
	}
	return nil
}

func (s *Store) FailTask(ctx context.Context, id int64, attempts int, lastErr string, retryAfter time.Duration, maxAttempts int) error {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	lastErr = truncateDBString(lastErr, 1000)
	if attempts >= maxAttempts {
		const q = `UPDATE scheduled_tasks SET status = 'failed', last_error = $2, updated_at = NOW() WHERE id = $1`
		_, err := s.pool.Exec(ctx, q, id, lastErr)
		if err != nil {
			return fmt.Errorf("db: mark task %d failed: %w", id, err)
		}
		return nil
	}
	if retryAfter <= 0 {
		retryAfter = time.Duration(attempts+1) * 10 * time.Second
	}
	const q = `
		UPDATE scheduled_tasks
		SET status = 'pending', run_at = NOW() + ($2::int * INTERVAL '1 second'), last_error = $3, updated_at = NOW()
		WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, id, int(retryAfter.Seconds()), lastErr)
	if err != nil {
		return fmt.Errorf("db: reschedule task %d: %w", id, err)
	}
	return nil
}

func isMetricColumn(column string) bool {
	switch column {
	case "messages_deleted", "spammers_kicked", "strikes_issued":
		return true
	default:
		return false
	}
}

func isSupportedLanguage(lang string) bool {
	switch lang {
	case "en", "km":
		return true
	default:
		return false
	}
}

func isSupportedTaskType(taskType models.ScheduledTaskType) bool {
	switch taskType {
	case models.TaskCaptchaExpire, models.TaskDeleteMessage, models.TaskUnmuteUser:
		return true
	default:
		return false
	}
}

func truncateDBString(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n]
}

func (s *Store) RecoverRunningTasks(ctx context.Context) (int64, error) {
	const q = `
		UPDATE scheduled_tasks
		SET status = 'pending', run_at = NOW(), updated_at = NOW(), last_error = 'recovered after bot restart'
		WHERE status = 'running'`
	cmd, err := s.pool.Exec(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("db: recover running tasks: %w", err)
	}
	return cmd.RowsAffected(), nil
}
