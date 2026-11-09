package sanitizer

import (
	"net/url"
	"strings"
)

// NormalizePlainText trims leading and trailing whitespace while preserving
// all literal code, comparison operators (e.g. 'load < 2 > 1'), and debugging
// snippets. The React UI renders comments and text fields strictly as text nodes,
// so HTML stripping is avoided to prevent loss of technical information.
func NormalizePlainText(s string) string {
	return strings.TrimSpace(s)
}

// SanitizePlainText alias for backwards compatibility.
func SanitizePlainText(s string) string {
	return NormalizePlainText(s)
}

// SanitizeURL validates that raw is an http or https URL and strictly rejects
// any credential-bearing URLs (containing userinfo). Returns empty string if invalid.
func SanitizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	// Strictly reject credential-bearing URLs (never store credentials)
	if u.User != nil {
		return ""
	}
	return u.String()
}

// NormalizeMap preserves map keys and values verbatim to guarantee identity
// in label sets and annotations while ensuring a non-nil map.
func NormalizeMap(m map[string]string) map[string]string {
	if m == nil {
		return make(map[string]string)
	}
	res := make(map[string]string, len(m))
	for k, v := range m {
		res[k] = v
	}
	return res
}

// SanitizeMap alias for backwards compatibility.
func SanitizeMap(m map[string]string) map[string]string {
	return NormalizeMap(m)
}
