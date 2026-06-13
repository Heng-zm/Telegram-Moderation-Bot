package cache

import (
	"sync"
	"time"

	"telemod/internal/models"
)

type GroupCache struct {
	groups    sync.Map // map[int64]*models.Group
	badWords  sync.Map // map[int64][]string
	whitelist sync.Map // map[int64][]string
}

func NewGroupCache() *GroupCache { return &GroupCache{} }

func (c *GroupCache) SetGroup(g *models.Group) {
	if g == nil {
		return
	}
	c.groups.Store(g.ChatID, cloneGroup(g))
}

func (c *GroupCache) GetGroup(chatID int64) *models.Group {
	v, ok := c.groups.Load(chatID)
	if !ok {
		return nil
	}
	g, ok := v.(*models.Group)
	if !ok || g == nil {
		c.groups.Delete(chatID)
		return nil
	}
	return cloneGroup(g)
}

func (c *GroupCache) DeleteGroup(chatID int64) {
	c.groups.Delete(chatID)
	c.badWords.Delete(chatID)
	c.whitelist.Delete(chatID)
}

func (c *GroupCache) UpdateGroup(chatID int64, fn func(*models.Group)) (*models.Group, bool) {
	if fn == nil {
		return nil, false
	}
	v, ok := c.groups.Load(chatID)
	if !ok {
		return nil, false
	}
	g, ok := v.(*models.Group)
	if !ok || g == nil {
		c.groups.Delete(chatID)
		return nil, false
	}
	updated := cloneGroup(g)
	fn(updated)
	c.groups.Store(chatID, cloneGroup(updated))
	return updated, true
}

func (c *GroupCache) SetBadWords(chatID int64, words []string) {
	c.badWords.Store(chatID, cloneStrings(words))
}

func (c *GroupCache) GetBadWords(chatID int64) []string {
	v, ok := c.badWords.Load(chatID)
	if !ok {
		return nil
	}
	words, ok := v.([]string)
	if !ok {
		c.badWords.Delete(chatID)
		return nil
	}
	return cloneStrings(words)
}

func (c *GroupCache) DeleteBadWords(chatID int64) { c.badWords.Delete(chatID) }

func (c *GroupCache) SetWhitelist(chatID int64, domains []string) {
	c.whitelist.Store(chatID, cloneStrings(domains))
}

func (c *GroupCache) GetWhitelist(chatID int64) []string {
	v, ok := c.whitelist.Load(chatID)
	if !ok {
		return nil
	}
	domains, ok := v.([]string)
	if !ok {
		c.whitelist.Delete(chatID)
		return nil
	}
	return cloneStrings(domains)
}

func (c *GroupCache) DeleteWhitelist(chatID int64) { c.whitelist.Delete(chatID) }

func cloneGroup(g *models.Group) *models.Group {
	if g == nil {
		return nil
	}
	cp := *g
	return &cp
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

type AdminCache struct {
	ttl time.Duration
	m   sync.Map // map[adminKey]adminValue
}

type adminKey struct {
	chatID int64
	userID int64
}

type adminValue struct {
	allowed   bool
	expiresAt time.Time
}

func NewAdminCache(ttl time.Duration) *AdminCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &AdminCache{ttl: ttl}
}

func (c *AdminCache) Get(chatID, userID int64) (bool, bool) {
	v, ok := c.m.Load(adminKey{chatID: chatID, userID: userID})
	if !ok {
		return false, false
	}
	entry, ok := v.(adminValue)
	if !ok || time.Now().After(entry.expiresAt) {
		c.m.Delete(adminKey{chatID: chatID, userID: userID})
		return false, false
	}
	return entry.allowed, true
}

func (c *AdminCache) Set(chatID, userID int64, allowed bool) {
	c.m.Store(adminKey{chatID: chatID, userID: userID}, adminValue{
		allowed:   allowed,
		expiresAt: time.Now().Add(c.ttl),
	})
}

func (c *AdminCache) Delete(chatID, userID int64) {
	c.m.Delete(adminKey{chatID: chatID, userID: userID})
}

type CaptchaStore struct {
	m sync.Map // map[captchaKey]*models.PendingCaptcha
}

type captchaKey struct {
	chatID int64
	userID int64
}

func NewCaptchaStore() *CaptchaStore { return &CaptchaStore{} }

func (s *CaptchaStore) Set(c *models.PendingCaptcha) {
	if c == nil {
		return
	}
	cp := *c
	s.m.Store(captchaKey{chatID: c.ChatID, userID: c.UserID}, &cp)
}

func (s *CaptchaStore) Get(chatID, userID int64) *models.PendingCaptcha {
	v, ok := s.m.Load(captchaKey{chatID: chatID, userID: userID})
	if !ok {
		return nil
	}
	p, ok := v.(*models.PendingCaptcha)
	if !ok || p == nil {
		s.m.Delete(captchaKey{chatID: chatID, userID: userID})
		return nil
	}
	cp := *p
	return &cp
}

func (s *CaptchaStore) Delete(chatID, userID int64) {
	s.m.Delete(captchaKey{chatID: chatID, userID: userID})
}

func (s *CaptchaStore) DeleteIfMessageID(chatID, userID int64, messageID int) (*models.PendingCaptcha, bool) {
	key := captchaKey{chatID: chatID, userID: userID}
	v, ok := s.m.Load(key)
	if !ok {
		return nil, false
	}
	p, ok := v.(*models.PendingCaptcha)
	if !ok || p == nil || p.MessageID != messageID {
		return nil, false
	}
	s.m.Delete(key)
	cp := *p
	return &cp, true
}

type FloodLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	cooldown time.Duration
	entries  map[floodKey]*floodEntry
}

type floodKey struct {
	chatID int64
	userID int64
}

type floodEntry struct {
	events     []floodEvent
	lastStrike time.Time
}

type floodEvent struct {
	at        time.Time
	messageID int
}

type FloodDecision struct {
	Flooded      bool
	ShouldStrike bool
	MessageIDs   []int
}

func NewFloodLimiter(limit int, window time.Duration) *FloodLimiter {
	if limit <= 0 {
		limit = 5
	}
	if window <= 0 {
		window = 3 * time.Second
	}
	return &FloodLimiter{
		limit:    limit,
		window:   window,
		cooldown: window,
		entries:  make(map[floodKey]*floodEntry),
	}
}

func (l *FloodLimiter) Add(chatID, userID int64, messageID int, now time.Time) FloodDecision {
	l.mu.Lock()
	defer l.mu.Unlock()

	key := floodKey{chatID: chatID, userID: userID}
	entry := l.entries[key]
	if entry == nil {
		entry = &floodEntry{}
		l.entries[key] = entry
	}

	cutoff := now.Add(-l.window)
	kept := entry.events[:0]
	for _, ev := range entry.events {
		if ev.at.After(cutoff) {
			kept = append(kept, ev)
		}
	}
	entry.events = append(kept, floodEvent{at: now, messageID: messageID})

	if len(entry.events) <= l.limit {
		return FloodDecision{}
	}

	ids := make([]int, 0, len(entry.events))
	seen := make(map[int]struct{}, len(entry.events))
	for _, ev := range entry.events {
		if ev.messageID == 0 {
			continue
		}
		if _, ok := seen[ev.messageID]; ok {
			continue
		}
		seen[ev.messageID] = struct{}{}
		ids = append(ids, ev.messageID)
	}

	shouldStrike := entry.lastStrike.IsZero() || now.Sub(entry.lastStrike) >= l.cooldown
	if shouldStrike {
		entry.lastStrike = now
	}

	return FloodDecision{Flooded: true, ShouldStrike: shouldStrike, MessageIDs: ids}
}

func (l *FloodLimiter) CleanupOlderThan(maxAge time.Duration) {
	if maxAge <= 0 {
		maxAge = time.Minute
	}
	cutoff := time.Now().Add(-maxAge)
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, entry := range l.entries {
		if entry == nil || (len(entry.events) == 0 && entry.lastStrike.Before(cutoff)) {
			delete(l.entries, key)
			continue
		}
		kept := entry.events[:0]
		for _, ev := range entry.events {
			if ev.at.After(cutoff) {
				kept = append(kept, ev)
			}
		}
		entry.events = kept
	}
}

type CooldownLimiter struct {
	mu       sync.Mutex
	cooldown time.Duration
	entries  map[cooldownKey]time.Time
}

type cooldownKey struct {
	chatID int64
	userID int64
}

func NewCooldownLimiter(cooldown time.Duration) *CooldownLimiter {
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &CooldownLimiter{cooldown: cooldown, entries: make(map[cooldownKey]time.Time)}
}

// Allow returns true when the user can perform the action now. If false, the
// returned duration is how long the user should wait before trying again.
func (l *CooldownLimiter) Allow(chatID, userID int64, now time.Time) (bool, time.Duration) {
	if l == nil || chatID == 0 || userID == 0 {
		return true, 0
	}
	if now.IsZero() {
		now = time.Now()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	key := cooldownKey{chatID: chatID, userID: userID}
	nextAllowed, ok := l.entries[key]
	if ok && now.Before(nextAllowed) {
		return false, nextAllowed.Sub(now)
	}
	l.entries[key] = now.Add(l.cooldown)
	return true, 0
}

func (l *CooldownLimiter) Cleanup() {
	if l == nil {
		return
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, nextAllowed := range l.entries {
		if now.After(nextAllowed) {
			delete(l.entries, key)
		}
	}
}
