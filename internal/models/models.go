package models

import "time"

type Group struct {
	ChatID          int64     `db:"chat_id"`
	Language        string    `db:"language"`
	CaptchaEnabled  bool      `db:"captcha_enabled"`
	LinksEnabled    bool      `db:"links_enabled"`
	StrikesEnabled  bool      `db:"strikes_enabled"`
	BlockPhotos     bool      `db:"block_photos"`
	BlockVideos     bool      `db:"block_videos"`
	BlockDocuments  bool      `db:"block_documents"`
	BlockAudio      bool      `db:"block_audio"`
	BlockVoice      bool      `db:"block_voice"`
	BlockStickers   bool      `db:"block_stickers"`
	BlockAnimations bool      `db:"block_animations"`
	BlockVideoNotes bool      `db:"block_video_notes"`
	BlockForwards   bool      `db:"block_forwards"`
	LogChannelID    int64     `db:"log_channel_id"`
	CreatedAt       time.Time `db:"created_at"`
}

type UserStrike struct {
	ID        int64     `db:"id"`
	ChatID    int64     `db:"chat_id"`
	UserID    int64     `db:"user_id"`
	Strikes   int       `db:"strikes"`
	UpdatedAt time.Time `db:"updated_at"`
}

type GroupStats struct {
	ChatID          int64     `db:"chat_id"`
	RecordDate      time.Time `db:"record_date"`
	MessagesDeleted int       `db:"messages_deleted"`
	SpammersKicked  int       `db:"spammers_kicked"`
	StrikesIssued   int       `db:"strikes_issued"`
	Language        string    `db:"language"`
	LogChannelID    int64     `db:"log_channel_id"`
}

type AuditEvent struct {
	ChatID    int64
	UserID    int64
	Username  string
	Content   string
	Reason    string
	Timestamp time.Time
}

type PendingCaptcha struct {
	ChatID    int64
	UserID    int64
	MessageID int
	ExpiresAt time.Time
}

type ScheduledTaskType string

const (
	TaskCaptchaExpire ScheduledTaskType = "captcha_expire"
	TaskDeleteMessage ScheduledTaskType = "delete_message"
	TaskUnmuteUser    ScheduledTaskType = "unmute_user"
)

type ScheduledTaskStatus string

const (
	TaskStatusPending   ScheduledTaskStatus = "pending"
	TaskStatusRunning   ScheduledTaskStatus = "running"
	TaskStatusDone      ScheduledTaskStatus = "done"
	TaskStatusFailed    ScheduledTaskStatus = "failed"
	TaskStatusCancelled ScheduledTaskStatus = "cancelled"
)

type ScheduledTask struct {
	ID        int64
	Type      ScheduledTaskType
	DedupKey  string
	Payload   []byte
	RunAt     time.Time
	Status    ScheduledTaskStatus
	Attempts  int
	LastError string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CaptchaExpirePayload struct {
	ChatID    int64 `json:"chat_id"`
	UserID    int64 `json:"user_id"`
	MessageID int   `json:"message_id"`
}

type DeleteMessagePayload struct {
	ChatID    int64 `json:"chat_id"`
	MessageID int   `json:"message_id"`
}

type UnmuteUserPayload struct {
	ChatID int64 `json:"chat_id"`
	UserID int64 `json:"user_id"`
}

type ViolationType int

const (
	ViolationBadWord ViolationType = iota
	ViolationLink
	ViolationMedia
	ViolationForward
	ViolationFlood
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
	case ViolationFlood:
		return "anti_flood"
	default:
		return "unknown"
	}
}
