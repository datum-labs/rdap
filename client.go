package rdapclient

import (
	"context"
	"net/http"
	"time"
)

// Doer is the minimal http.Client interface we depend on (handy for tests/mocks).
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client is a concurrency-safe RDAP client with bootstrap & caching.
type Client struct {
	// HTTP / defaults
	hc          Doer
	ua          string
	baseTimeout time.Duration
	headerExtra http.Header

	// sources
	bootstrapURL    string // IANA DNS bootstrap
	ipBootstrapURL  string // IANA IP bootstrap
	asnBootstrapURL string // IANA ASN bootstrap

	// caches
	rdapBaseCache *ttlCache[string] // tld -> base URL
	respCache     *respCache        // url -> cachedResponse

	// behavior
	maxRetries int
	backoff    Backoff
	now        func() time.Time
}

// New returns a ready Client with good defaults.
func New(opts ...Option) *Client {
	c := &Client{
		hc:              defaultHTTPClient(),
		ua:              "rdapclient/0.1 (+https://example.invalid)",
		baseTimeout:     10 * time.Second,
		bootstrapURL:    "https://data.iana.org/rdap/dns.json",
		ipBootstrapURL:  "https://data.iana.org/rdap/ipv4.json", // covers v4 and v6 via ipv6.json; see options
		asnBootstrapURL: "https://data.iana.org/rdap/asn.json",
		headerExtra:     make(http.Header),

		rdapBaseCache: newTTLCache[string](6*time.Hour, 64),
		respCache:     newRespCache(512, 10*time.Minute),

		maxRetries: 2,
		backoff:    ExponentialBackoff(200*time.Millisecond, 2.0, 2*time.Second),
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func defaultHTTPClient() *http.Client { return &http.Client{Timeout: 15 * time.Second} }

// RefreshBootstrap forces a re-fetch of IANA DNS bootstrap right now.
func (c *Client) RefreshBootstrap(ctx context.Context) error { return c.fetchBootstrap(ctx, true) }
