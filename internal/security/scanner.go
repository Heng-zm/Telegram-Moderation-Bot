package security

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var urlCandidateRE = regexp.MustCompile(`(?i)(https?://[^\s<>]+|www\.[^\s<>]+|[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+(?::\d{2,5})?(?:/[^\s<>]*)?)`)

func levenshtein(a, b []rune) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

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

func MatchesEnglish(text string, badWords []string) (bool, string) {
	tokens := tokeniseWestern(text)
	if len(tokens) == 0 || len(badWords) == 0 {
		return false, ""
	}

	exactBadWords := make(map[string]string, len(badWords))
	normalizedBadWords := make([]string, 0, len(badWords))
	for _, raw := range badWords {
		bw := normalizeWesternToken(raw)
		if bw == "" {
			continue
		}
		exactBadWords[bw] = raw
		normalizedBadWords = append(normalizedBadWords, bw)
	}

	for _, token := range tokens {
		if original, ok := exactBadWords[token]; ok {
			return true, original
		}
	}

	for _, token := range tokens {
		tRunes := []rune(token)
		for _, bw := range normalizedBadWords {
			bwRunes := []rune(bw)
			threshold := maxEditDistance(len(bwRunes))
			if abs(len(tRunes)-len(bwRunes)) > threshold {
				continue
			}
			if levenshtein(tRunes, bwRunes) <= threshold {
				return true, exactBadWords[bw]
			}
		}
	}
	return false, ""
}

func tokeniseWestern(text string) []string {
	text = strings.ToLower(text)
	var raw []string
	var cur strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else if cur.Len() > 0 {
			raw = append(raw, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		raw = append(raw, cur.String())
	}

	tokens := make([]string, 0, len(raw)+4)
	tokens = append(tokens, raw...)

	for i := 0; i < len(raw); {
		if utf8.RuneCountInString(raw[i]) == 1 {
			j := i + 1
			for j < len(raw) && utf8.RuneCountInString(raw[j]) == 1 {
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

func normalizeWesternToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func MatchesKhmer(text string, badWords []string) (bool, string) {
	if text == "" || len(badWords) == 0 {
		return false, ""
	}
	for _, bw := range badWords {
		bw = strings.TrimSpace(bw)
		if bw != "" && strings.Contains(text, bw) {
			return true, bw
		}
	}
	return false, ""
}

func ScanMessage(text, language string, badWords []string) (bool, string) {
	if len(badWords) == 0 || utf8.RuneCountInString(text) == 0 {
		return false, ""
	}
	if language == "km" {
		return MatchesKhmer(text, badWords)
	}
	return MatchesEnglish(text, badWords)
}

func ExtractHostnames(text string) []string {
	matches := urlCandidateRE.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(matches))
	hosts := make([]string, 0, len(matches))
	for _, candidate := range matches {
		host := normalizeHost(candidate)
		if host == "" {
			continue
		}
		if _, exists := seen[host]; exists {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	return hosts
}

func normalizeHost(candidate string) string {
	raw := strings.TrimSpace(candidate)
	raw = strings.Trim(raw, " \t\n\r.,;:!?)()]}'\"")
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(raw), "www.") {
		raw = "https://" + raw
	}
	if !strings.HasPrefix(strings.ToLower(raw), "http://") && !strings.HasPrefix(strings.ToLower(raw), "https://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if strings.HasPrefix(host, "www.") {
		host = strings.TrimPrefix(host, "www.")
	}
	return host
}

func IsAllowedDomain(host string, whitelist []string) bool {
	host = normalizeDomain(host)
	if host == "" {
		return false
	}
	for _, allowed := range whitelist {
		allowed = normalizeDomain(allowed)
		if allowed == "" {
			continue
		}
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

func ContainsBlockedLink(text string, whitelist []string) bool {
	hosts := ExtractHostnames(text)
	if len(hosts) == 0 {
		return false
	}
	for _, h := range hosts {
		if !IsAllowedDomain(h, whitelist) {
			return true
		}
	}
	return false
}

func normalizeDomain(domain string) string {
	domain = strings.TrimSpace(strings.ToLower(domain))
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "www.")
	if idx := strings.IndexAny(domain, "/?#:"); idx >= 0 {
		domain = domain[:idx]
	}
	return strings.TrimSuffix(domain, ".")
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
