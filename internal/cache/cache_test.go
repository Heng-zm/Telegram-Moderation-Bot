package cache

import (
	"testing"
	"time"

	"telemod/internal/models"
)

func TestGroupCacheReturnsCopies(t *testing.T) {
	c := NewGroupCache()
	c.SetGroup(&models.Group{ChatID: 123, Language: "en", CaptchaEnabled: true})

	first := c.GetGroup(123)
	if first == nil {
		t.Fatal("expected cached group")
	}
	first.Language = "km"
	first.CaptchaEnabled = false

	second := c.GetGroup(123)
	if second == nil {
		t.Fatal("expected cached group on second read")
	}
	if second.Language != "en" || !second.CaptchaEnabled {
		t.Fatalf("cache leaked mutable pointer: got language=%q captcha=%v", second.Language, second.CaptchaEnabled)
	}
}

func TestFloodLimiterSlidingWindow(t *testing.T) {
	limiter := NewFloodLimiter(5, 3*time.Second)
	now := time.Unix(1000, 0)
	for i := 1; i <= 5; i++ {
		decision := limiter.Add(-100, 42, i, now.Add(time.Duration(i)*100*time.Millisecond))
		if decision.Flooded {
			t.Fatalf("message %d should not flood yet", i)
		}
	}

	decision := limiter.Add(-100, 42, 6, now.Add(700*time.Millisecond))
	if !decision.Flooded {
		t.Fatal("expected flood on sixth message inside window")
	}
	if !decision.ShouldStrike {
		t.Fatal("first flood should trigger strike")
	}
	if len(decision.MessageIDs) != 6 {
		t.Fatalf("expected 6 messages to delete, got %d", len(decision.MessageIDs))
	}

	decision = limiter.Add(-100, 42, 7, now.Add(900*time.Millisecond))
	if !decision.Flooded {
		t.Fatal("expected continued flood inside window")
	}
	if decision.ShouldStrike {
		t.Fatal("cooldown should prevent immediate duplicate strike")
	}

	decision = limiter.Add(-100, 42, 8, now.Add(5*time.Second))
	if decision.Flooded {
		t.Fatal("old events should expire outside the sliding window")
	}
}
