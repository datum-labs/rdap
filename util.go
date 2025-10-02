package rdapclient

import (
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

func lastLabel(domain string) string {
	domain = strings.TrimSuffix(domain, ".")
	parts := strings.Split(domain, ".")
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(parts[len(parts)-1])
}

func trimDotLower(s string) string { return strings.ToLower(strings.TrimPrefix(s, ".")) }

func mustJoin(base, p1 string, more ...string) string {
	u, _ := url.Parse(base)
	u.Path = path.Join(u.Path, p1)
	for _, m := range more {
		u.Path = path.Join(u.Path, m)
	}
	return u.String()
}

func errorsAs(err error, target interface{}) bool { return errors.As(err, target) }

func lower(s string) string { return strings.ToLower(s) }

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func copyHeaders(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

func retryAfter(h http.Header, fallback time.Duration) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		if sec, err := time.ParseDuration(strings.TrimSpace(v) + "s"); err == nil {
			if sec > 0 && sec < 10*time.Second {
				return sec
			}
		}
		if t, err := time.Parse(time.RFC1123, v); err == nil {
			if d := time.Until(t); d > 0 && d < 10*time.Second {
				return d
			}
		}
	}
	return fallback
}

// temporary reports whether err (or any wrapped error) implements Temporary() bool and returns true.
func temporary(err error) bool {
	type temp interface{ Temporary() bool }
	// Direct type assertion
	if te, ok := err.(temp); ok && te.Temporary() {
		return true
	}
	// Walk wrapped errors
	for {
		u := errors.Unwrap(err)
		if u == nil {
			return false
		}
		if te, ok := u.(temp); ok && te.Temporary() {
			return true
		}
		err = u
	}
}

// toStringSlice converts an interface{} holding a []any into []string (best-effort).
func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
