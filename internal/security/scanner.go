package security

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// ── Levenshtein Distance ─────────────────────────────────────────────────────

// levenshtein computes the edit distance between two rune slices.
func levenshtein(a, b []rune) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	// Rolling two-row DP to keep memory O(n).
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// maxEditDistance returns the allowed edit distance for a given rune length:
// ≤4 runes → 0 errors (exact), ≤8 runes → 1 error, longer → 2 errors.
func maxEditDistance(runes int) int {
	switch {
	case runes < 5:
		return 0
	case runes < 9:
		return 1
	default:
		return 2
	}
}

// ── Western Token Fuzzy Matching ─────────────────────────────────────────────

// MatchesEnglish returns true if any token in the normalised text is within
// the allowed edit distance of any prohibited word.
func MatchesEnglish(text string, badWords []string) (bool, string) {
	tokens := tokeniseWestern(text)
	for _, token := range tokens {
		tRunes := []rune(token)
		for _, bw := range badWords {
			bwRunes := []rune(bw)
			threshold := maxEditDistance(len(bwRunes))
			if levenshtein(tRunes, bwRunes) <= threshold {
				return true, bw
			}
		}
	}
	return false, ""
}

// tokeniseWestern lowercases and splits on non-alphabetic characters so that
// "SPAM", "baaad" etc. surface as single candidate tokens.
// It also reconstructs dot/dash bypass attempts like "s.p.a.m" by
// merging sequences of adjacent single-character tokens into a combined token.
func tokeniseWestern(text string) []string {
	text = strings.ToLower(text)
	var raw []string
	var cur strings.Builder
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else {
			if cur.Len() > 0 {
				raw = append(raw, cur.String())
				cur.Reset()
			}
		}
	}
	if cur.Len() > 0 {
		raw = append(raw, cur.String())
	}

	// Also try merging runs of consecutive single-character tokens to catch
	// separator-bypass patterns like "s.p.a.m" → ["s","p","a","m"] → "spam".
	tokens := make([]string, 0, len(raw)+4)
	tokens = append(tokens, raw...)

	i := 0
	for i < len(raw) {
		if len(raw[i]) == 1 {
			j := i + 1
			for j < len(raw) && len(raw[j]) == 1 {
				j++
			}
			if j-i > 1 {
				tokens = append(tokens, strings.Join(raw[i:j], ""))
			}
			i = j
		} else {
			i++
		}
	}
	return tokens
}

// ── Khmer Substring Scanning ─────────────────────────────────────────────────

// MatchesKhmer returns true when any prohibited word appears as a direct
// substring of the text. Khmer script has no whitespace between words, so
// tokenisation is not viable.
func MatchesKhmer(text string, badWords []string) (bool, string) {
	for _, bw := range badWords {
		if strings.Contains(text, bw) {
			return true, bw
		}
	}
	return false, ""
}

// ── Unified Scanner ──────────────────────────────────────────────────────────

// ScanMessage selects the appropriate algorithm based on the group language
// and returns whether a violation was found together with the matched word.
func ScanMessage(text, language string, badWords []string) (bool, string) {
	if len(badWords) == 0 || utf8.RuneCountInString(text) == 0 {
		return false, ""
	}
	if language == "km" {
		return MatchesKhmer(text, badWords)
	}
	return MatchesEnglish(text, badWords)
}

// ── Link Extraction ──────────────────────────────────────────────────────────

// ExtractHostnames parses all URLs found in text and returns their hostnames.
func ExtractHostnames(text string) []string {
	var hosts []string
	for _, word := range strings.Fields(text) {
		if !strings.Contains(word, ".") {
			continue
		}
		// Ensure scheme so url.Parse works.
		raw := word
		if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
			raw = "https://" + raw
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			continue
		}
		hosts = append(hosts, strings.ToLower(u.Hostname()))
	}
	return hosts
}

// IsAllowedDomain returns true if host matches one of the whitelisted domains
// (exact match or is a subdomain of a whitelisted base domain).
func IsAllowedDomain(host string, whitelist []string) bool {
	for _, allowed := range whitelist {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

// ContainsBlockedLink returns true if the text contains any URL whose host is
// not in the whitelist. A non-empty whitelist implies link-checking is active.
func ContainsBlockedLink(text string, whitelist []string) bool {
	hosts := ExtractHostnames(text)
	for _, h := range hosts {
		if !IsAllowedDomain(h, whitelist) {
			return true
		}
	}
	return false
}
