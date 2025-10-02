package rdapclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// getJSON performs a GET with validators, caching, retries & rate-limit handling.
func (c *Client) getJSON(ctx context.Context, u string) (map[string]any, http.Header, error) {
	// strong cache hit (fresh TTL)
	if body, ok := c.respCache.Get(u); ok {
		var m map[string]any
		if err := json.Unmarshal(body, &m); err == nil {
			return m, nil, nil
		}
	}

	useValidators := true     // send ETag/Last-Modified initially
	didUnconditional := false // ensure we only try once without validators

	for attempt := 1; ; attempt++ {
		reqCtx, cancel := context.WithTimeout(ctx, c.baseTimeout)

		req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
		req.Header.Set("Accept", "application/rdap+json, application/json;q=0.8, */*;q=0.1")
		req.Header.Set("User-Agent", c.ua)
		copyHeaders(req.Header, c.headerExtra)

		if useValidators {
			if meta, ok := c.respCache.Meta(u); ok {
				if meta.ETag != "" {
					req.Header.Set("If-None-Match", meta.ETag)
				}
				if !meta.LastModified.IsZero() {
					req.Header.Set("If-Modified-Since", meta.LastModified.Format(http.TimeFormat))
				}
			}
		}

		resp, err := c.hc.Do(req)
		if err != nil {
			cancel()
			if attempt <= c.maxRetries && isRetryableNetErr(err) {
				select {
				case <-time.After(c.backoff(attempt)):
					continue
				case <-ctx.Done():
					return nil, nil, ctx.Err()
				}
			}
			return nil, nil, err
		}

		switch resp.StatusCode {
		case http.StatusNotModified:
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			cancel()

			if body := c.respCache.FreshBody(u); body != nil {
				var m map[string]any
				if json.Unmarshal(body, &m) == nil {
					c.respCache.UpdateFreshness(u, resp.Header)
					return m, resp.Header, nil
				}
			}

			// No cached body: drop validators once and retry unconditionally.
			if !didUnconditional {
				didUnconditional = true
				useValidators = false
				continue
			}
			return nil, nil, fmt.Errorf("rdap GET %s: 304 but no cached body", u)

		case http.StatusOK:
			b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			cancel()
			if err != nil {
				return nil, nil, err
			}
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				return nil, nil, err
			}
			c.respCache.Store(u, b, resp.Header)
			return m, resp.Header, nil

		case http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout, http.StatusInternalServerError:
			wait := retryAfter(resp.Header, c.backoff(attempt))
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			cancel()
			if attempt <= c.maxRetries {
				select {
				case <-time.After(wait):
					continue
				case <-ctx.Done():
					return nil, nil, ctx.Err()
				}
			}
			return nil, nil, fmt.Errorf("rdap GET %s: %s", u, resp.Status)

		default:
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
			resp.Body.Close()
			cancel()
			if resp.StatusCode == http.StatusNotFound {
				c.respCache.StoreNegative(u, 5*time.Minute)
			}
			return nil, nil, fmt.Errorf("rdap GET %s: %s: %s", u, resp.Status, string(b))
		}
	}
}

func isRetryableNetErr(err error) bool {
	var ne net.Error
	if errorsAs(err, &ne) && (ne.Timeout() || temporary(ne)) {
		return true
	}
	msg := lower(err.Error())
	return containsAny(msg, "connection reset", "broken pipe", "unexpected eof", "no such host")
}
