// lookup.go
package rdapclient

import (
	"context"
	"net/netip"
	"regexp"
	"strings"
)

var (
	reASN    = regexp.MustCompile(`^(?i:AS)?\d+$`)
	reNSHost = regexp.MustCompile(`(?i)^(ns\d+|dns\d+)[.-]`) // cheap heuristic
)

// Lookup auto-detects the query type and calls the appropriate RDAP method.
// Optionally pass a tldHint for Entity lookups (can be "").
func (c *Client) Lookup(ctx context.Context, q string, tldHint string) (any, error) {
	s := strings.TrimSpace(q)

	// 1) ASN: "AS15169" or "15169"
	if reASN.MatchString(s) {
		return c.Autnum(ctx, s)
	}

	// 2) IP or CIDR
	if pfx, err := netip.ParsePrefix(s); err == nil {
		// Normalize form (avoid mixed-case or stray spaces)
		return c.IP(ctx, pfx.String())
	}
	if ip, err := netip.ParseAddr(s); err == nil {
		return c.IP(ctx, ip.String())
	}

	// 3) Nameserver host heuristic (still a domain – try Nameserver first)
	ls := strings.ToLower(s)
	switch {
	case reNSHost.MatchString(ls):
		if ns, err := c.Nameserver(ctx, ls); err == nil {
			return ns, nil
		}
		// fall through to Domain if nameserver path 404s at registry
	}

	// 4) Entity handle (registry-specific). If caller passed a hint, try Entity.
	// Common entity handles contain '-' or all-caps alpha + digits (e.g., "ORG-EXAMPLE1").
	if tldHint != "" && looksLikeEntityHandle(ls) {
		if e, err := c.Entity(ctx, s, tldHint); err == nil {
			return e, nil
		}
		// fall back to domain next
	}

	// 5) Default: treat as FQDN domain
	return c.Domain(ctx, ls)
}

func looksLikeEntityHandle(s string) bool {
	// very permissive: contains dash or ends with digits and has an alpha prefix
	if strings.Contains(s, "-") {
		return true
	}
	// e.g., ORGEXAMPLE123
	hasAlpha := false
	hasDigit := false
	for _, r := range s {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			hasAlpha = true
		}
		if r >= '0' && r <= '9' {
			hasDigit = true
		}
	}
	return hasAlpha && hasDigit
}
