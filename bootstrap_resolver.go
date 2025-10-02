package rdapclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

type bootstrapServices struct {
	Services [][]any `json:"services"`
}

// resolveBaseFromBootstrapDNS loads dns.json and returns the base for a tld (lowercase, no dot).
func (c *Client) resolveBaseFromBootstrapDNS(ctx context.Context, tld string) (string, error) {
	if tld == "" {
		return "", fmt.Errorf("empty TLD")
	}
	tld = strings.ToLower(strings.TrimPrefix(tld, "."))
	if base, ok := c.rdapBaseCache.Get(tld); ok {
		return base, nil
	}
	if err := c.fetchBootstrap(ctx, false); err != nil {
		// Fall back to default base if bootstrap fetch fails
		if c.defaultRDAPBase != "" {
			return c.defaultRDAPBase, nil
		}
		return "", err
	}
	if base, ok := c.rdapBaseCache.Get(tld); ok {
		return base, nil
	}
	// Try a forced refresh once (handles 304-without-body case or first-run without cache)
	if err := c.fetchBootstrap(ctx, true); err == nil {
		if base, ok := c.rdapBaseCache.Get(tld); ok {
			return base, nil
		}
	}
	// Not found in bootstrap after refresh: fall back to default base
	if c.defaultRDAPBase != "" {
		return c.defaultRDAPBase, nil
	}
	return "", fmt.Errorf("no RDAP base for TLD %q", tld)
}

// fetchBootstrapGeneric fetches a bootstrap json (dns/asn/ipv4/ipv6) and returns parsed services & response meta caching.
func (c *Client) fetchBootstrapGeneric(ctx context.Context, url string) (*bootstrapServices, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.baseTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", c.ua)
	copyHeaders(req.Header, c.headerExtra)

	// Conditional
	if meta, ok := c.respCache.Meta(url); ok {
		if meta.ETag != "" {
			req.Header.Set("If-None-Match", meta.ETag)
		}
		if !meta.LastModified.IsZero() {
			req.Header.Set("If-Modified-Since", meta.LastModified.Format(http.TimeFormat))
		}
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		// Return cached-but-parsed? We didn’t keep the body; simplest is refetch w/ force when needed.
		// For our usage (single pass), treat as soft miss and force next time if needed.
		return nil, fmt.Errorf("bootstrap 304 Not Modified (no cached body)")
	case http.StatusOK:
		body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2MB cap
		if err != nil {
			return nil, err
		}
		var bs bootstrapServices
		if err := json.Unmarshal(body, &bs); err != nil {
			return nil, fmt.Errorf("parse bootstrap: %w", err)
		}
		c.respCache.StoreMeta(url, resp.Header)
		return &bs, nil
	default:
		return nil, fmt.Errorf("bootstrap fetch failed: %s", resp.Status)
	}
}

// resolveBaseFromBootstrapASN resolves an RDAP base for a numeric ASN using IANA asn.json.
// It supports single ASNs and ASN ranges "X-Y".
func (c *Client) resolveBaseFromBootstrapASN(ctx context.Context, asn uint64) (string, error) {
	// Try cache hit first
	key := fmt.Sprintf("asn:%d", asn)
	if base, ok := c.rdapBaseCache.Get(key); ok {
		return base, nil
	}

	bs, err := c.fetchBootstrapGeneric(ctx, c.asnBootstrapURL)
	if err != nil {
		// fall back to rdap.org as a compliant aggregator
		return "https://rdap.org", nil
	}

	for _, svc := range bs.Services {
		if len(svc) != 2 {
			continue
		}
		ranges := toStringSlice(svc[0])
		urls := toStringSlice(svc[1])
		if len(urls) == 0 {
			continue
		}
		base := strings.TrimRight(urls[0], "/")
		for _, r := range ranges {
			// r is either a single number "12345" or a range "1-1876"
			lo, hi, ok := parseASNRange(r)
			if !ok {
				continue
			}
			if asn >= lo && asn <= hi {
				// cache a small windowed key to avoid exploding cache
				c.rdapBaseCache.Set(key, base)
				return base, nil
			}
		}
	}
	// not found: use rdap.org
	return "https://rdap.org", nil
}

func parseASNRange(s string) (uint64, uint64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, false
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		lo, err1 := strconv.ParseUint(strings.TrimSpace(s[:i]), 10, 64)
		hi, err2 := strconv.ParseUint(strings.TrimSpace(s[i+1:]), 10, 64)
		if err1 != nil || err2 != nil || hi < lo {
			return 0, 0, false
		}
		return lo, hi, true
	}
	// single
	x, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return x, x, true
}

// resolveBaseFromBootstrapIP resolves a base for a single IP or CIDR using ipv4/ipv6 bootstrap.
// We match by CIDR containment.
func (c *Client) resolveBaseFromBootstrapIP(ctx context.Context, ipOrCIDR string) (string, error) {
	// Normalize to an address we can test containment with
	var addr netip.Addr
	if p, err := netip.ParsePrefix(ipOrCIDR); err == nil {
		addr = p.Addr()
	} else {
		a, err := netip.ParseAddr(ipOrCIDR)
		if err != nil {
			return "", err
		}
		addr = a
	}

	// Select file
	bootstrapURL := c.ipBootstrapURL
	is6 := addr.Is6()
	// If the configured ipBootstrapURL is the opposite family, redirect to the right file.
	if is6 && strings.HasSuffix(bootstrapURL, "/ipv4.json") {
		bootstrapURL = "https://data.iana.org/rdap/ipv6.json"
	}
	if !is6 && strings.HasSuffix(bootstrapURL, "/ipv6.json") {
		bootstrapURL = "https://data.iana.org/rdap/ipv4.json"
	}

	// Try a tiny LRU key cache
	key := "ip:" + addr.String()
	if base, ok := c.rdapBaseCache.Get(key); ok {
		return base, nil
	}

	bs, err := c.fetchBootstrapGeneric(ctx, bootstrapURL)
	if err != nil {
		return "https://rdap.org", nil
	}

	var bestBase string
	var bestMask int = -1 // longest prefix match

	for _, svc := range bs.Services {
		if len(svc) != 2 {
			continue
		}
		cidrs := toStringSlice(svc[0])
		urls := toStringSlice(svc[1])
		if len(urls) == 0 {
			continue
		}
		base := strings.TrimRight(urls[0], "/")

		for _, raw := range cidrs {
			raw = strings.TrimSpace(raw)
			// Service entries can be single addresses, but IANA ip bootstrap uses CIDRs.
			pfx, err := netip.ParsePrefix(raw)
			if err != nil {
				continue
			}
			// Family must match
			if pfx.Addr().Is6() != is6 {
				continue
			}
			if pfx.Contains(addr) {
				ones := pfx.Bits()
				if ones > bestMask {
					bestMask = ones
					bestBase = base
				}
			}
		}
	}
	if bestBase != "" {
		c.rdapBaseCache.Set(key, bestBase)
		return bestBase, nil
	}
	return "https://rdap.org", nil
}
