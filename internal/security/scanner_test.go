package security_test

import (
	"testing"

	"telemod/internal/security"
)

func TestMatchesEnglish_ExactMatch(t *testing.T) {
	ok, word := security.MatchesEnglish("buy cheap spam now", []string{"spam"})
	if !ok || word != "spam" {
		t.Fatalf("expected exact spam match, ok=%v word=%q", ok, word)
	}
}

func TestMatchesEnglish_DotBypass(t *testing.T) {
	ok, _ := security.MatchesEnglish("s.p.a.m me now", []string{"spam"})
	if !ok {
		t.Fatal("expected dotted bypass match")
	}
}

func TestMatchesEnglish_FuzzyOneEdit(t *testing.T) {
	ok, _ := security.MatchesEnglish("badwurd this", []string{"badword"})
	if !ok {
		t.Fatal("expected fuzzy match")
	}
}

func TestMatchesEnglish_ShortWordExact(t *testing.T) {
	ok, _ := security.MatchesEnglish("spamm this", []string{"spam"})
	if ok {
		t.Fatal("short words should require exact match")
	}
}

func TestMatchesEnglish_NoViolation(t *testing.T) {
	ok, _ := security.MatchesEnglish("hello world", []string{"spam", "badword"})
	if ok {
		t.Fatal("expected clean text")
	}
}

func TestMatchesKhmer_SubstringHit(t *testing.T) {
	bad := "ពាក្យអាក្រក់"
	text := "នៅក្នុង" + bad + "ដែល"
	ok, word := security.MatchesKhmer(text, []string{bad})
	if !ok || word != bad {
		t.Fatalf("expected Khmer substring match, ok=%v word=%q", ok, word)
	}
}

func TestMatchesKhmer_NoHit(t *testing.T) {
	ok, _ := security.MatchesKhmer("ការសួរជំរាបសួរ", []string{"ពាក្យអាក្រក់"})
	if ok {
		t.Fatal("expected no Khmer match")
	}
}

func TestExtractHostnames(t *testing.T) {
	hosts := security.ExtractHostnames("check https://evil.com, http://sub.safe.org/path?q=1 and www.example.com")
	if len(hosts) != 3 {
		t.Fatalf("expected 3 hosts, got %d: %v", len(hosts), hosts)
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
		t.Fatal("expected no blocked link")
	}
}

func TestContainsBlockedLink_BlockedLink(t *testing.T) {
	if !security.ContainsBlockedLink("visit http://evil.com now", []string{"safe.org"}) {
		t.Fatal("expected blocked link")
	}
}

func TestContainsBlockedLink_AllowedLink(t *testing.T) {
	if security.ContainsBlockedLink("visit https://safe.org/page", []string{"safe.org"}) {
		t.Fatal("expected whitelisted link to pass")
	}
}

func TestContainsBlockedLink_EmptyWhitelistBlocksLinks(t *testing.T) {
	if !security.ContainsBlockedLink("visit https://anywhere.example", nil) {
		t.Fatal("expected empty whitelist to block links when link filter is enabled by caller")
	}
}
