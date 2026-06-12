package cache

import (
	"sync"
	"telemod/internal/models"
)

// GroupCache provides thread-safe in-memory storage for group configurations,
// bad words, and whitelisted domains. It eliminates DB round-trips on the
// hot message processing path.
type GroupCache struct {
	groups    sync.Map // map[int64]*models.Group
	badWords  sync.Map // map[int64][]string
	whitelist sync.Map // map[int64][]string
	mu        sync.RWMutex
}

// NewGroupCache creates an initialised GroupCache.
func NewGroupCache() *GroupCache {
	return &GroupCache{}
}

// SetGroup upserts a group config atomically.
func (c *GroupCache) SetGroup(g *models.Group) {
	c.groups.Store(g.ChatID, g)
}

// GetGroup retrieves a group config. Returns nil if not cached.
func (c *GroupCache) GetGroup(chatID int64) *models.Group {
	v, ok := c.groups.Load(chatID)
	if !ok {
		return nil
	}
	return v.(*models.Group)
}

// DeleteGroup evicts a group and its associated data.
func (c *GroupCache) DeleteGroup(chatID int64) {
	c.groups.Delete(chatID)
	c.badWords.Delete(chatID)
	c.whitelist.Delete(chatID)
}

// SetBadWords stores the bad-word list for a group.
func (c *GroupCache) SetBadWords(chatID int64, words []string) {
	c.badWords.Store(chatID, words)
}

// GetBadWords returns cached bad words for a group, or nil.
func (c *GroupCache) GetBadWords(chatID int64) []string {
	v, ok := c.badWords.Load(chatID)
	if !ok {
		return nil
	}
	return v.([]string)
}

// SetWhitelist stores whitelisted domains for a group.
func (c *GroupCache) SetWhitelist(chatID int64, domains []string) {
	c.whitelist.Store(chatID, domains)
}

// GetWhitelist returns cached whitelisted domains, or nil.
func (c *GroupCache) GetWhitelist(chatID int64) []string {
	v, ok := c.whitelist.Load(chatID)
	if !ok {
		return nil
	}
	return v.([]string)
}

// UpdateGroupField applies an in-place mutation to a cached group config
// under write-lock so concurrent reads stay consistent.
func (c *GroupCache) UpdateGroupField(chatID int64, fn func(*models.Group)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.groups.Load(chatID)
	if !ok {
		return
	}
	fn(v.(*models.Group))
}

// CaptchaStore is a concurrent tracker for pending CAPTCHA challenges.
type CaptchaStore struct {
	m sync.Map // map[captchaKey]*models.PendingCaptcha
}

type captchaKey struct {
	chatID int64
	userID int64
}

func NewCaptchaStore() *CaptchaStore {
	return &CaptchaStore{}
}

// Set stores a pending captcha.
func (s *CaptchaStore) Set(c *models.PendingCaptcha) {
	s.m.Store(captchaKey{c.ChatID, c.UserID}, c)
}

// Get retrieves a pending captcha. Returns nil if absent.
func (s *CaptchaStore) Get(chatID, userID int64) *models.PendingCaptcha {
	v, ok := s.m.Load(captchaKey{chatID, userID})
	if !ok {
		return nil
	}
	return v.(*models.PendingCaptcha)
}

// Delete removes a pending captcha after resolution.
func (s *CaptchaStore) Delete(chatID, userID int64) {
	s.m.Delete(captchaKey{chatID, userID})
}
