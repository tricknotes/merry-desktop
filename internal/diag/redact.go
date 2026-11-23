// Package diag builds the diagnostic report (F-14). Every file in the report
// goes through Redact, so credentials, tokens and proxy passwords never end up
// in the zip (REQUIREMENTS.md 4.2 and CLAUDE.md).
package diag

import (
	"regexp"
	"strings"
)

const mask = "xxxxx"

var (
	// scheme://user:password@host
	urlCredentials = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]+):[^\s@/]+@`)
	// Authorization: Bearer abc / Basic abc, and X-Registry-Auth: ...
	authHeader = regexp.MustCompile(`(?i)((?:proxy-)?authorization|x-registry-auth)(["']?\s*[:=]\s*["']?)(?:(bearer|basic)\s+)?[^\s"',]+`)
	// key=value, key: value, "key": "value" for secret-looking keys.
	secretField = regexp.MustCompile(`(?i)("?[a-z0-9_.-]*(?:password|passwd|passphrase|secret|token|apikey|api_key|access_key|private_key|credential|auth)[a-z0-9_.-]*"?)(\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;}]+)`)
)

// Redact masks secrets in text. extra are known secret values (for example the
// detected proxy URL) that are masked wherever they appear.
func Redact(text string, extra ...string) string {
	for _, s := range extra {
		if len(s) >= 4 {
			text = strings.ReplaceAll(text, s, mask)
		}
	}
	text = urlCredentials.ReplaceAllString(text, "${1}:"+mask+"@")
	text = authHeader.ReplaceAllStringFunc(text, func(m string) string {
		sub := authHeader.FindStringSubmatch(m)
		scheme := ""
		if sub[3] != "" {
			scheme = sub[3] + " "
		}
		return sub[1] + sub[2] + scheme + mask
	})
	text = secretField.ReplaceAllStringFunc(text, func(m string) string {
		sub := secretField.FindStringSubmatch(m)
		value := sub[3]
		key := strings.ToLower(sub[1])
		if strings.Contains(key, "authorization") || strings.Contains(key, "registry-auth") || strings.Trim(value, `"'`) == mask {
			return m // already handled by authHeader
		}
		switch {
		case strings.HasPrefix(value, `"`):
			return sub[1] + sub[2] + `"` + mask + `"`
		case strings.HasPrefix(value, `'`):
			return sub[1] + sub[2] + `'` + mask + `'`
		}
		return sub[1] + sub[2] + mask
	})
	return text
}
