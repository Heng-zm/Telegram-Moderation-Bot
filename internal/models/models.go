package models

import "time"

// Group represents a multi-tenant Telegram group configuration.
type Group struct {
	ChatID         int64     `db:"chat_id"`
	Language       string    `db:"language"`        // "en" or "km"
	CaptchaEnabled bool      `db:"captcha_enabled"`
	LinksEnabled   bool      `db:"links_enabled"`
	StrikesEnabled bool      `db:"strikes_enabled"`
	BlockMedia     bool      `db:"block_media"`
	BlockForwards  bool      `db:"block_forwards"`
	LogChannelID   int64     `db:"log_channel_id"`
	CreatedAt      time.Time `db:"created_at"`
}

// UserStrike tracks how many violations a user has accumulated per group.
type UserStrike struct {
	ID        int64     `db:"id"`
	ChatID    int64     `db:"chat_id"`
	UserID    int64     `db:"user_id"`
	Strikes   int       `db:"strikes"`
	UpdatedAt time.Time `db:"updated_at"`
}

// GroupStats holds a 24-hour aggregate for a single group.
type GroupStats struct {
	ChatID           int64     `db:"chat_id"`
	RecordDate       time.Time `db:"record_date"`
	MessagesDeleted  int       `db:"messages_deleted"`
	SpammersKicked   int       `db:"spammers_kicked"`
	StrikesIssued    int       `db:"strikes_issued"`
}

// AuditEvent is the payload routed to the async log channel.
type AuditEvent struct {
	ChatID    int64
	UserID    int64
	Username  string
	Content   string
	Reason    string
	Timestamp time.Time
}

// PendingCaptcha tracks a user awaiting CAPTCHA verification.
type PendingCaptcha struct {
	ChatID    int64
	UserID    int64
	MessageID int
	ExpiresAt time.Time
}

// ViolationType is an enum for the type of rule broken.
type ViolationType int

const (
	ViolationBadWord ViolationType = iota
	ViolationLink
	ViolationMedia
	ViolationForward
)

func (v ViolationType) String() string {
	switch v {
	case ViolationBadWord:
		return "prohibited_word"
	case ViolationLink:
		return "unauthorized_link"
	case ViolationMedia:
		return "blocked_media"
	case ViolationForward:
		return "blocked_forward"
	default:
		return "unknown"
	}
}
