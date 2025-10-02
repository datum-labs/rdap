package rdapclient

import "time"

type Option func(*Client)

func WithHTTPDoer(d Doer) Option          { return func(c *Client) { c.hc = d } }
func WithUserAgent(ua string) Option      { return func(c *Client) { c.ua = ua } }
func WithTimeout(d time.Duration) Option  { return func(c *Client) { c.baseTimeout = d } }
func WithBootstrapURL(u string) Option    { return func(c *Client) { c.bootstrapURL = u } }
func WithIPBootstrapURL(u string) Option  { return func(c *Client) { c.ipBootstrapURL = u } }
func WithASNBootstrapURL(u string) Option { return func(c *Client) { c.asnBootstrapURL = u } }
func WithMaxRetries(n int) Option         { return func(c *Client) { c.maxRetries = n } }
func WithBackoff(b Backoff) Option        { return func(c *Client) { c.backoff = b } }
func WithHeader(k, v string) Option       { return func(c *Client) { c.headerExtra.Add(k, v) } }
func WithCacheSizes(tldCap, entityCap int) Option {
	return func(c *Client) {
		if tldCap > 0 {
			c.rdapBaseCache.Resize(tldCap)
		}
		if entityCap > 0 {
			c.respCache.Resize(entityCap)
		}
	}
}
