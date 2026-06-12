package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"telemod/internal/models"
)

// Store wraps the pgx connection pool and exposes all data-access methods.
type Store struct {
	pool *pgxpool.Pool
}

// New creates a Store backed by a pgx connection pool.
func New(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: parse config: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: connect pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases pool resources.
func (s *Store) Close() { s.pool.Close() }

// ── Group ────────────────────────────────────────────────────────────────────

// GetOrCreateGroup fetches an existing group config or inserts a default one.
func (s *Store) GetOrCreateGroup(ctx context.Context, chatID int64) (*models.Group, error) {
	const q = `
		INSERT INTO groups (chat_id) VALUES ($1)
		ON CONFLICT (chat_id) DO NOTHING`
	_, _ = s.pool.Exec(ctx, q, chatID)

	return s.GetGroup(ctx, chatID)
}

// GetGroup retrieves a group config by chat_id.
func (s *Store) GetGroup(ctx context.Context, chatID int64) (*models.Group, error) {
	const q = `
		SELECT chat_id, language, captcha_enabled, links_enabled,
		       strikes_enabled, block_media, block_forwards, log_channel_id, created_at
		FROM groups WHERE chat_id = $1`

	row := s.pool.QueryRow(ctx, q, chatID)
	g := &models.Group{}
	err := row.Scan(
		&g.ChatID, &g.Language, &g.CaptchaEnabled, &g.LinksEnabled,
		&g.StrikesEnabled, &g.BlockMedia, &g.BlockForwards, &g.LogChannelID, &g.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("db: get group %d: %w", chatID, err)
	}
	return g, nil
}

// UpdateGroup persists every configurable column for a group.
func (s *Store) UpdateGroup(ctx context.Context, g *models.Group) error {
	const q = `
		UPDATE groups SET
			language         = $2,
			captcha_enabled  = $3,
			links_enabled    = $4,
			strikes_enabled  = $5,
			block_media      = $6,
			block_forwards   = $7,
			log_channel_id   = $8
		WHERE chat_id = $1`
	_, err := s.pool.Exec(ctx, q,
		g.ChatID, g.Language, g.CaptchaEnabled, g.LinksEnabled,
		g.StrikesEnabled, g.BlockMedia, g.BlockForwards, g.LogChannelID,
	)
	return err
}

// ── Bad Words ────────────────────────────────────────────────────────────────

// GetBadWords returns all prohibited words for a chat.
func (s *Store) GetBadWords(ctx context.Context, chatID int64) ([]string, error) {
	const q = `SELECT word FROM bad_words WHERE chat_id = $1`
	rows, err := s.pool.Query(ctx, q, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// AddBadWord inserts a prohibited word (idempotent).
func (s *Store) AddBadWord(ctx context.Context, chatID int64, word string) error {
	const q = `INSERT INTO bad_words (chat_id, word) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err := s.pool.Exec(ctx, q, chatID, word)
	return err
}

// RemoveBadWord deletes a prohibited word.
func (s *Store) RemoveBadWord(ctx context.Context, chatID int64, word string) error {
	const q = `DELETE FROM bad_words WHERE chat_id = $1 AND word = $2`
	_, err := s.pool.Exec(ctx, q, chatID, word)
	return err
}

// ── Link Whitelist ───────────────────────────────────────────────────────────

// GetWhitelist returns whitelisted domains for a chat.
func (s *Store) GetWhitelist(ctx context.Context, chatID int64) ([]string, error) {
	const q = `SELECT domain FROM link_whitelist WHERE chat_id = $1`
	rows, err := s.pool.Query(ctx, q, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// AddWhitelistDomain inserts a domain (idempotent).
func (s *Store) AddWhitelistDomain(ctx context.Context, chatID int64, domain string) error {
	const q = `INSERT INTO link_whitelist (chat_id, domain) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err := s.pool.Exec(ctx, q, chatID, domain)
	return err
}

// ── Strikes ──────────────────────────────────────────────────────────────────

// IncrementStrike atomically adds one strike and returns the new total.
func (s *Store) IncrementStrike(ctx context.Context, chatID, userID int64) (int, error) {
	const q = `
		INSERT INTO user_strikes (chat_id, user_id, strikes, updated_at)
		VALUES ($1, $2, 1, NOW())
		ON CONFLICT (chat_id, user_id) DO UPDATE
			SET strikes    = user_strikes.strikes + 1,
			    updated_at = NOW()
		RETURNING strikes`

	var total int
	err := s.pool.QueryRow(ctx, q, chatID, userID).Scan(&total)
	return total, err
}

// DeleteUserStrike wipes all strike records for a user (post-ban cleanup).
func (s *Store) DeleteUserStrike(ctx context.Context, chatID, userID int64) error {
	const q = `DELETE FROM user_strikes WHERE chat_id = $1 AND user_id = $2`
	_, err := s.pool.Exec(ctx, q, chatID, userID)
	return err
}

// ── Stats ────────────────────────────────────────────────────────────────────

// TrackMetric atomically increments one of the three daily stat counters.
// column must be one of: "messages_deleted", "spammers_kicked", "strikes_issued".
func (s *Store) TrackMetric(ctx context.Context, chatID int64, column string) error {
	// column is an internal constant – safe to interpolate.
	q := fmt.Sprintf(`
		INSERT INTO group_stats (chat_id, record_date, %s)
		VALUES ($1, CURRENT_DATE, 1)
		ON CONFLICT (chat_id, record_date) DO UPDATE
			SET %s = group_stats.%s + 1`, column, column, column)
	_, err := s.pool.Exec(ctx, q, chatID)
	return err
}

// GetDailyStats returns today's stats for all groups.
func (s *Store) GetDailyStats(ctx context.Context) ([]*models.GroupStats, error) {
	const q = `
		SELECT chat_id, record_date, messages_deleted, spammers_kicked, strikes_issued
		FROM group_stats
		WHERE record_date = CURRENT_DATE`

	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.GroupStats
	for rows.Next() {
		gs := &models.GroupStats{}
		if err := rows.Scan(&gs.ChatID, &gs.RecordDate, &gs.MessagesDeleted,
			&gs.SpammersKicked, &gs.StrikesIssued); err != nil {
			return nil, err
		}
		out = append(out, gs)
	}
	return out, rows.Err()
}

// GetAllGroups returns every registered group (used by the daily stats cron).
func (s *Store) GetAllGroups(ctx context.Context) ([]*models.Group, error) {
	const q = `SELECT chat_id, language, captcha_enabled, links_enabled,
		               strikes_enabled, block_media, block_forwards, log_channel_id, created_at
		        FROM groups`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.Group
	for rows.Next() {
		g := &models.Group{}
		if err := rows.Scan(&g.ChatID, &g.Language, &g.CaptchaEnabled, &g.LinksEnabled,
			&g.StrikesEnabled, &g.BlockMedia, &g.BlockForwards, &g.LogChannelID, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
