package rdapclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (c *Client) rdapBaseForDomain(ctx context.Context, fqdn string) (string, error) {
	return c.rdapBaseForTLD(ctx, lastLabel(fqdn))
}

func (c *Client) rdapBaseForTLD(ctx context.Context, tld string) (string, error) {
	return c.resolveBaseFromBootstrapDNS(ctx, tld)
}

func (c *Client) fetchBootstrap(ctx context.Context, force bool) error {
	reqCtx, cancel := context.WithTimeout(ctx, c.baseTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, c.bootstrapURL, nil)
	req.Header.Set("User-Agent", c.ua)
	copyHeaders(req.Header, c.headerExtra)

	// conditional
	if meta, ok := c.respCache.Meta(c.bootstrapURL); ok && !force {
		if meta.ETag != "" {
			req.Header.Set("If-None-Match", meta.ETag)
		}
		if !meta.LastModified.IsZero() {
			req.Header.Set("If-Modified-Since", meta.LastModified.Format(http.TimeFormat))
		}
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		return nil
	case http.StatusOK:
		body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		if err != nil {
			return err
		}
		var obj struct {
			Services [][]any `json:"services"`
		}
		if err := json.Unmarshal(body, &obj); err != nil {
			return fmt.Errorf("parse bootstrap: %w", err)
		}

		for _, svc := range obj.Services {
			if len(svc) != 2 {
				continue
			}
			tlds := toStringSlice(svc[0])
			urls := toStringSlice(svc[1])
			if len(urls) == 0 {
				continue
			}
			base := strings.TrimRight(urls[0], "/")
			for _, tl := range tlds {
				c.rdapBaseCache.Set(strings.ToLower(tl), base)
			}
		}
		c.respCache.StoreMeta(c.bootstrapURL, resp.Header)
		return nil
	default:
		return fmt.Errorf("bootstrap fetch failed: %s", resp.Status)
	}
}
