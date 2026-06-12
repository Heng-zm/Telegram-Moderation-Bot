package security_test

import (
	"testing"

	"telemod/internal/security"
)

// ── Levenshtein / Western matching ────────────────────────────────────────────

func TestMatchesEnglish_ExactMatch(t *testing.T) {
	ok, word := security.MatchesEnglish("buy cheap spam now", []string{"spam"})
	if !ok {
		t.Fatal("expected match for exact word 'spam'")
	}
	if word != "spam" {
		t.Fatalf("expected matched word 'spam', got %q", word)
	}
}

func TestMatchesEnglish_DotBypass(t *testing.T) {
	// "s.p.a.m" should tokenise to "spam" and match.
	ok, _ := security.MatchesEnglish("s.p.a.m me now", []string{"spam"})
	if !ok {
		t.Fatal("expected match for dotted bypass 's.p.a.m'")
	}
}

func TestMatchesEnglish_FuzzyOneEdit(t *testing.T) {
	// "spamm" has edit distance 1 from "spam" (length 4 → threshold 0).
	// "badword" length 7 → threshold 1 → "badwurd" should match.
	ok, _ := security.MatchesEnglish("badwurd this", []string{"badword"})
	if !ok {
		t.Fatal("expected fuzzy match for 'badwurd' ~ 'badword'")
	}
}

func TestMatchesEnglish_ShortWordExact(t *testing.T) {
	// Words < 5 chars require exact match (threshold 0).
	ok, _ := security.MatchesEnglish("spamm this", []string{"spam"})
	if ok {
		t.Fatal("expected NO match: 'spamm' has distance 1 from 'spam' but threshold is 0 for len<5")
	}
}

func TestMatchesEnglish_NoViolation(t *testing.T) {
	ok, _ := security.MatchesEnglish("hello world", []string{"spam", "badword"})
	if ok {
		t.Fatal("expected no violation in clean text")
	}
}

// ── Khmer substring scanner ───────────────────────────────────────────────────

func TestMatchesKhmer_SubstringHit(t *testing.T) {
	// Khmer text containing the bad word as a substring.
	bad := "ពាក្យអាក្រក់"
	text := "នៅក្នុង" + bad + "ដែល"
	ok, word := security.MatchesKhmer(text, []string{bad})
	if !ok {
		t.Fatal("expected Khmer substring match")
	}
	if word != bad {
		t.Fatalf("expected matched word %q, got %q", bad, word)
	}
}

func TestMatchesKhmer_NoHit(t *testing.T) {
	ok, _ := security.MatchesKhmer("ការសួរជំរាបសួរ", []string{"ពាក្យអាក្រក់"})
	if ok {
		t.Fatal("expected no Khmer match in clean text")
	}
}

// ── Link extraction ───────────────────────────────────────────────────────────

func TestExtractHostnames(t *testing.T) {
	text := "check https://evil.com and http://sub.safe.org/path?q=1"
	hosts := security.ExtractHostnames(text)
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d: %v", len(hosts), hosts)
	}
}

func TestIsAllowedDomain_ExactMatch(t *testing.T) {
	if !security.IsAllowedDomain("safe.org", []string{"safe.org"}) {
		t.Fatal("exact domain should be allowed")
	}
}

func TestIsAllowedDomain_Subdomain(t *testing.T) {
	if !security.IsAllowedDomain("docs.safe.org", []string{"safe.org"}) {
		t.Fatal("subdomain should be allowed")
	}
}

func TestIsAllowedDomain_NotAllowed(t *testing.T) {
	if security.IsAllowedDomain("evil.com", []string{"safe.org"}) {
		t.Fatal("evil.com should not be whitelisted")
	}
}

func TestContainsBlockedLink_NoLinks(t *testing.T) {
	if security.ContainsBlockedLink("hello no links here", []string{"safe.org"}) {
		t.Fatal("expected no blocked link in clean text")
	}
}

func TestContainsBlockedLink_BlockedLink(t *testing.T) {
	if !security.ContainsBlockedLink("visit http://evil.com now", []string{"safe.org"}) {
		t.Fatal("expected blocked link detected")
	}
}

func TestContainsBlockedLink_AllowedLink(t *testing.T) {
	if security.ContainsBlockedLink("visit https://safe.org/page", []string{"safe.org"}) {
		t.Fatal("expected whitelisted link to pass")
	}
}
