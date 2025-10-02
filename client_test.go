package rdapclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ---------- Backoff ----------

func TestExponentialBackoff_DefaultsAndClamping(t *testing.T) {
	// Defaults kick in when invalid inputs.
	b := ExponentialBackoff(0, 0, 0)
	// start=100ms, factor=1.5, max=2s
	got1 := b(1)  // 100ms
	got2 := b(2)  // 150ms
	got3 := b(10) // grows but clamps to <= 2s
	if got1 != 100*time.Millisecond {
		t.Fatalf("attempt 1: want 100ms, got %v", got1)
	}
	if got2 != 150*time.Millisecond {
		t.Fatalf("attempt 2: want 150ms, got %v", got2)
	}
	if got3 > 2*time.Second {
		t.Fatalf("clamp: want <= 2s, got %v", got3)
	}

	// Custom values
	b = ExponentialBackoff(200*time.Millisecond, 2.0, 1*time.Second)
	// 200ms, 400ms, 800ms, 1.6s->clamped to 1s
	wants := []time.Duration{200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, 1 * time.Second}
	for i, w := range wants {
		if got := b(i + 1); got != w {
			t.Fatalf("attempt %d: want %v, got %v", i+1, w, got)
		}
	}
}

// ---------- ttlCache ----------

func TestTTLCache_GetSet_ExpireAndEvict(t *testing.T) {
	c := newTTLCache[int](time.Minute, 2)
	// Freeze time by overriding now.
	base := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	c.now = func() time.Time { return base }

	c.Set("a", 1)
	c.Set("b", 2)

	// Fresh
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("fresh a miss: %v %v", v, ok)
	}

	// Insert c -> evicts LRU ("b" is most recent after Get("a") moved it? We moved "a" to front.)
	// Access "a" so "b" becomes LRU.
	if _, ok := c.Get("a"); !ok {
		t.Fatalf("a should be present")
	}
	c.Set("c", 3)

	if _, ok := c.Get("b"); ok {
		t.Fatalf("b should have been evicted")
	}
	if v, ok := c.Get("c"); !ok || v != 3 {
		t.Fatalf("c missing after insert/evict")
	}

	// Expiration
	c.now = func() time.Time { return base.Add(2 * time.Minute) }
	if _, ok := c.Get("a"); ok {
		t.Fatalf("a should be expired")
	}
}

// ---------- respCache ----------

func TestRespCache_StoreGet_NegativeAndMetaUpdate(t *testing.T) {
	rc := newRespCache(2, 30*time.Second)
	// Freeze time
	base := time.Date(2024, 2, 1, 10, 0, 0, 0, time.UTC)
	rc.now = func() time.Time { return base }

	h := make(http.Header)
	h.Set("Cache-Control", "max-age=60")
	h.Set("ETag", `"v1"`)
	rc.Store("https://x", []byte(`{"ok":true}`), h)

	// Fresh get
	if b, ok := rc.Get("https://x"); !ok || !strings.Contains(string(b), "ok") {
		t.Fatalf("fresh get failed: %v %v", ok, string(b))
	}

	// UpdateFreshness should push expiry forward and keep ETag
	h2 := make(http.Header)
	h2.Set("Cache-Control", "max-age=120")
	h2.Set("ETag", `"v2"`)
	rc.UpdateFreshness("https://x", h2)
	m, ok := rc.Meta("https://x")
	if !ok || m.ETag != `"v2"` {
		t.Fatalf("meta not merged: %+v", m)
	}

	// Negative cache should cause misses until negUntil
	rc.StoreNegative("https://neg", 1*time.Hour)
	if _, ok := rc.Get("https://neg"); ok {
		t.Fatalf("negative cache should miss while active")
	}
	// Advance time past negUntil
	rc.now = func() time.Time { return base.Add(2 * time.Hour) }
	if _, ok := rc.Get("https://neg"); ok {
		t.Fatalf("negative cache should be treated as miss (no body), not hit")
	}

	// Eviction correctness (URL as key)
	rc = newRespCache(1, 10*time.Second)
	rc.Store("u1", []byte("1"), nil)
	rc.Store("u2", []byte("2"), nil) // evicts u1
	if _, ok := rc.Get("u1"); ok {
		t.Fatalf("u1 should be evicted")
	}
}

// ---------- helpers ----------

func TestRetryAfter(t *testing.T) {
	h := make(http.Header)
	h.Set("Retry-After", "3")
	if d := retryAfter(h, 10*time.Second); d != 3*time.Second {
		t.Fatalf("seconds form: want 3s, got %v", d)
	}
	// RFC1123 date, but clamp to <10s to be honored.
	when := time.Now().Add(5 * time.Second).UTC().Format(time.RFC1123)
	h2 := make(http.Header)
	h2.Set("Retry-After", when)
	if d := retryAfter(h2, 10*time.Second); d < 4*time.Second || d > 6*time.Second {
		t.Fatalf("date form: unexpected %v", d)
	}
	// Too large -> fallback
	h3 := make(http.Header)
	h3.Set("Retry-After", "999")
	if d := retryAfter(h3, 7*time.Second); d != 7*time.Second {
		t.Fatalf("fallback expected, got %v", d)
	}
}

func TestCopyHeaders(t *testing.T) {
	src := make(http.Header)
	src.Add("K", "a")
	src.Add("K", "b")
	dst := make(http.Header)
	copyHeaders(dst, src)
	if got := dst.Values("K"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("copyHeaders mismatch: %v", got)
	}
}

func TestLastLabelAndJoin(t *testing.T) {
	if got := lastLabel("Sub.Example.COM."); got != "com" {
		t.Fatalf("lastLabel: got %q", got)
	}
	base := "https://rdap.example.com/"
	joined := mustJoin(base, "/domain/", "example.com")
	u, err := url.Parse(joined)
	if err != nil || !strings.HasSuffix(u.String(), "/domain/example.com") {
		t.Fatalf("mustJoin unexpected: %v %v", u, err)
	}
}

func TestToStringSlice(t *testing.T) {
	in := []any{"COM", 1, "net", struct{}{}}
	got := toStringSlice(in)
	if !reflect.DeepEqual(got, []string{"COM", "net"}) {
		t.Fatalf("toStringSlice: %v", got)
	}
}

// ---------- ParseObject ----------

func TestParseObject_SwitchAndValidation(t *testing.T) {
	// Minimal valid Domain
	d := map[string]any{
		"objectClassName": "domain",
		"ldhName":         "example.com",
	}
	obj, err := ParseObject(d)
	if err != nil {
		t.Fatalf("domain parse err: %v", err)
	}
	if obj.GetObjectClassName() != "domain" {
		t.Fatalf("unexpected class: %s", obj.GetObjectClassName())
	}

	// Unknown class
	if _, err := ParseObject(map[string]any{"objectClassName": "weird"}); err == nil {
		t.Fatalf("expected error for unknown class")
	}
}

// ---------- Bootstrap & rdapBaseForTLD ----------

func TestRDAPBaseForTLD_BootstrapFetchAndCache(t *testing.T) {
	// Fake IANA dns.json
	var etag = `"abc"`
	lastMod := time.Now().Add(-1 * time.Hour).UTC().Format(http.TimeFormat)
	var hits int

	bootstrapJSON := `{
	  "services": [
	    [
	      ["COM","net"],
	      ["https://rdap.example/v1/"]
	    ],
	    [
	      ["org"],
	      ["https://org.example/rdap"]
	    ]
	  ]
	}`
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if inm := r.Header.Get("If-None-Match"); inm != "" {
			// Simulate 304 on conditional
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", lastMod)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, bootstrapJSON)
	}))
	defer s.Close()

	c := New(
		WithBootstrapURL(s.URL),
	)
	// Freeze cache clocks for determinism
	c.respCache.now = func() time.Time { return time.Now() }
	c.rdapBaseCache.now = func() time.Time { return time.Now() }

	// First call -> fetches and caches
	got, err := c.rdapBaseForTLD(context.Background(), "COM")
	if err != nil {
		t.Fatalf("rdapBaseForTLD error: %v", err)
	}
	// trimmed trailing slash
	if got != "https://rdap.example/v1" {
		t.Fatalf("base mismatch: %q", got)
	}

	// A second call should be satisfied from rdapBaseCache without another fetch.
	got2, err := c.rdapBaseForTLD(context.Background(), ".net")
	if err != nil || got2 != "https://rdap.example/v1" {
		t.Fatalf("cache miss or base mismatch: %v %q", err, got2)
	}

	// Force a conditional request path inside fetchBootstrap by asking for an unknown TLD,
	// which triggers fetchBootstrap again (but server will return 304 due to validators).
	// Preload meta so If-None-Match is sent.
	h := make(http.Header)
	h.Set("ETag", etag)
	h.Set("Last-Modified", lastMod)
	c.respCache.StoreMeta(c.bootstrapURL, h)

	_, _ = c.rdapBaseForTLD(context.Background(), "org") // this exists, but ensures another call path
	if hits < 1 {
		t.Fatalf("server should have been hit at least once, hits=%d", hits)
	}
}

// ---------- getJSON (caching, validators, errors, retry path) ----------

func TestGetJSON_CacheThenConditional304(t *testing.T) {
	var etag = `"v1"`
	lastMod := time.Now().Add(-2 * time.Hour).UTC().Format(http.TimeFormat)

	bodyV1 := `{"objectClassName":"domain","ldhName":"example.com"}`
	var requests int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		// If-None-Match present? Return 304
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", lastMod)
		w.Header().Set("Cache-Control", "max-age=60")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, bodyV1)
	}))
	defer ts.Close()

	c := New()
	c.backoff = func(int) time.Duration { return 0 }

	// freeze resp cache clock
	fixed := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	c.respCache.now = func() time.Time { return fixed }

	ctx := context.Background()
	u := ts.URL + "/domain/example.com"

	// First GET -> 200, store in cache
	m, hdr, err := c.getJSON(ctx, u)
	if err != nil {
		t.Fatalf("first getJSON err: %v", err)
	}
	if hdr.Get("ETag") != etag {
		t.Fatalf("want ETag in hdr")
	}
	if m["ldhName"] != "example.com" {
		t.Fatalf("parsed body mismatch: %v", m)
	}

	// Make the strong TTL stale so we actually send a conditional request.
	c.respCache.now = func() time.Time { return fixed.Add(2 * time.Minute) }

	// Second GET -> 304 path uses cached body and UpdateFreshness
	m2, _, err := c.getJSON(ctx, u)
	if err != nil {
		t.Fatalf("second getJSON err: %v", err)
	}
	if m2["ldhName"] != "example.com" {
		t.Fatalf("cached parse mismatch: %v", m2)
	}

	if requests < 2 {
		t.Fatalf("expected at least 2 requests, got %d", requests)
	}
}

func TestGetJSON_404StoresNegative(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	c := New()
	c.respCache.now = func() time.Time { return time.Unix(0, 0) }

	_, _, err := c.getJSON(context.Background(), ts.URL+"/nope")
	if err == nil {
		t.Fatalf("expected error for 404")
	}
	// Negative cache active => immediate miss in Get()
	if _, ok := c.respCache.Get(ts.URL + "/nope"); ok {
		t.Fatalf("negative cache should cause misses")
	}
}

// ---------- Entity/Domain high-level entrypoints (smoke) ----------

func TestDomain_Smoke(t *testing.T) {
	var srvURL string // will be filled after server starts

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/dns.json"):
			bootstrap := fmt.Sprintf(`{"services":[[["example"],["%s/"]]]}`, srvURL)
			w.Header().Set("Cache-Control", "max-age=60")
			_, _ = io.WriteString(w, bootstrap)
		case strings.HasPrefix(r.URL.Path, "/domain/"):
			domain := `{"objectClassName":"domain","ldhName":"example.example"}`
			w.Header().Set("Cache-Control", "max-age=60")
			_, _ = io.WriteString(w, domain)
		default:
			http.NotFound(w, r)
		}
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()
	srvURL = ts.URL // set after server is created

	c := New(WithBootstrapURL(ts.URL + "/dns.json"))

	d, err := c.Domain(context.Background(), "example.example")
	if err != nil {
		t.Fatalf("Domain() err: %v", err)
	}
	if d.LDHName != "example.example" {
		t.Fatalf("unexpected domain: %+v", d)
	}
}

// ---------- Misc net error helpers ----------

type tempErr struct{ msg string }

func (e tempErr) Error() string   { return e.msg }
func (e tempErr) Temporary() bool { return true }

func TestTemporaryHelper(t *testing.T) {
	// direct
	if !temporary(tempErr{"boom"}) {
		t.Fatalf("expected true for direct Temporary()")
	}
	// wrapped
	if !temporary(fmt.Errorf("wrap: %w", tempErr{"boom"})) {
		t.Fatalf("expected true for wrapped Temporary()")
	}
}

func TestIsRetryableNetErr_StringMatch(t *testing.T) {
	// We can't easily synthesize net.Error with Timeout() here, but string matcher is covered.
	errs := []error{
		fmt.Errorf("connection reset by peer"),
		fmt.Errorf("BROKEN PIPE"),
		fmt.Errorf("unexpected EOF while reading"),
		fmt.Errorf("no such host x"),
	}
	for _, e := range errs {
		if !isRetryableNetErr(e) {
			t.Fatalf("should be retryable: %v", e)
		}
	}
}

// ---------- JSON decodeInto round-trip ----------

func TestDecodeInto_RoundTrip(t *testing.T) {
	in := map[string]any{
		"objectClassName": "entity",
		"handle":          "H",
		"roles":           []any{"registrar"},
	}
	var e Entity
	if err := decodeInto(in, &e); err != nil {
		t.Fatalf("decodeInto err: %v", err)
	}
	if lower(e.ObjectClassName) != "entity" || e.Handle != "H" {
		t.Fatalf("unexpected decode: %+v", e)
	}

	// Double-check round-trip matches through ParseObject
	obj, err := ParseObject(in)
	if err != nil {
		t.Fatalf("ParseObject err: %v", err)
	}
	if obj.GetObjectClassName() != "entity" {
		t.Fatalf("unexpected class: %s", obj.GetObjectClassName())
	}
}

// ---------- Utility: ensure JSON bodies we generate are valid ----------

func TestParseObject_AllKnownClasses_Success(t *testing.T) {
	tests := []struct {
		name string
		obj  map[string]any
		want string
	}{
		{"entity", map[string]any{"objectClassName": "entity", "handle": "E"}, "entity"},
		{"domain", map[string]any{"objectClassName": "DoMaIn", "ldhName": "example.com"}, "domain"}, // case-insensitive
		{"nameserver", map[string]any{"objectClassName": "nameserver", "ldhName": "ns1.example.com"}, "nameserver"},
		{"ip network", map[string]any{"objectClassName": "ip network", "ipVersion": "v4"}, "ip network"},
		{"autnum", map[string]any{"objectClassName": "autnum", "startAutnum": int64(64512)}, "autnum"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := ParseObject(tt.obj)
			if err != nil {
				t.Fatalf("ParseObject err: %v", err)
			}
			if got := obj.GetObjectClassName(); strings.ToLower(got) != tt.want {
				t.Fatalf("objectClassName mismatch: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestParseObject_NilAndUnknownAndMissing(t *testing.T) {
	// nil map
	if _, err := ParseObject(nil); err == nil || !strings.Contains(err.Error(), "nil RDAP object") {
		t.Fatalf("expected nil RDAP object error, got %v", err)
	}

	// unknown class
	if _, err := ParseObject(map[string]any{"objectClassName": "weird"}); err == nil ||
		!strings.Contains(err.Error(), "unknown RDAP objectClassName: weird") {
		t.Fatalf("expected unknown class error, got %v", err)
	}

	// missing objectClassName -> ocn == "" => unknown error mentioning empty ocn
	if _, err := ParseObject(map[string]any{"ldhName": "example.com"}); err == nil ||
		!strings.Contains(err.Error(), "unknown RDAP objectClassName: ") {
		t.Fatalf("expected unknown class for missing objectClassName, got %v", err)
	}
}

func TestParseObject_DecodeErrorPath(t *testing.T) {
	// Force json.Marshal to fail by including an unsupported type (chan).
	m := map[string]any{
		"objectClassName": "domain",
		"bad":             make(chan int),
	}
	_, err := ParseObject(m)
	if err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("expected decode error due to unsupported type, got %v", err)
	}
}

func TestGetJSON_304NoCachedBody_UnconditionalRetrySuccess(t *testing.T) {
	var hits int
	body := `{"objectClassName":"domain","ldhName":"example.com"}`
	etag := `"v1"`

	// Server: if validators are present -> 304; else -> 200 with body
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Cache-Control", "max-age=60")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	defer ts.Close()

	c := New()
	c.backoff = func(int) time.Duration { return 0 }

	// Preload meta so getJSON sends validators, but DO NOT store a body.
	h := make(http.Header)
	h.Set("ETag", etag)
	h.Set("Last-Modified", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat))
	u := ts.URL + "/domain/example.com"
	c.respCache.StoreMeta(u, h)

	m, _, err := c.getJSON(context.Background(), u)
	if err != nil {
		t.Fatalf("getJSON err: %v", err)
	}
	if m["ldhName"] != "example.com" {
		t.Fatalf("unexpected json: %v", m)
	}
	if hits != 2 {
		t.Fatalf("expected 2 requests (304 then 200), got %d", hits)
	}
}

func TestGetJSON_304NoCachedBody_TwiceError(t *testing.T) {
	var hits int

	// Server always returns 304 (even when we drop validators on retry) to force the error.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusNotModified)
	}))
	defer ts.Close()

	c := New()
	c.backoff = func(int) time.Duration { return 0 }

	// Preload meta so we send validators on the first try, but no cached body exists.
	h := make(http.Header)
	h.Set("ETag", `"v1"`)
	h.Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	u := ts.URL + "/thing"
	c.respCache.StoreMeta(u, h)

	_, _, err := c.getJSON(context.Background(), u)
	if err == nil || !strings.Contains(err.Error(), "304 but no cached body") {
		t.Fatalf("expected specific 304 error, got %v", err)
	}
	// Should have tried twice: first 304, then unconditional (still 304) -> error
	if hits != 2 {
		t.Fatalf("expected 2 requests, got %d", hits)
	}
}

func TestGetJSON_RetryOn5xxThenSuccess(t *testing.T) {
	var hits int
	body := `{"objectClassName":"domain","ldhName":"ok.example"}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch hits {
		case 1, 2:
			// Return 503 with small Retry-After so we exercise retryAfter()
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		default:
			w.Header().Set("Cache-Control", "max-age=60")
			_, _ = io.WriteString(w, body)
		}
	}))
	defer ts.Close()

	c := New()
	c.maxRetries = 3
	c.backoff = func(int) time.Duration { return 0 } // instant between retries

	m, _, err := c.getJSON(context.Background(), ts.URL+"/x")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if m["ldhName"] != "ok.example" {
		t.Fatalf("parsed body mismatch: %v", m)
	}
	if hits != 3 {
		t.Fatalf("expected 3 hits (503,503,200), got %d", hits)
	}
}

func TestGetJSON_RetryExhaustsThenError(t *testing.T) {
	var hits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusBadGateway) // 502
	}))
	defer ts.Close()

	c := New()
	c.maxRetries = 2
	c.backoff = func(int) time.Duration { return 0 }

	_, _, err := c.getJSON(context.Background(), ts.URL+"/x")
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("expected 502 error after retries, got %v", err)
	}
	// First try + 2 retries = 3 total responses handled, but the function returns after the final 502;
	// the request count we can assert is >= 3 (exact count is 3 here).
	if hits != 3 {
		t.Fatalf("expected 3 attempts, got %d", hits)
	}
}

func TestGetJSON_RetryCanceledContext(t *testing.T) {
	var hits int
	firstHit := make(chan struct{}, 1)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			firstHit <- struct{}{}
		}
		w.WriteHeader(http.StatusServiceUnavailable) // 503 to enter retry path
	}))
	defer ts.Close()

	c := New()
	c.maxRetries = 5
	c.backoff = func(int) time.Duration { return 2 * time.Second } // ensure we wait in select

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel right after the first response is received, before the retry wait elapses.
	go func() {
		<-firstHit
		cancel()
	}()

	_, _, err := c.getJSON(ctx, ts.URL+"/x")
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if hits != 1 {
		t.Fatalf("expected 1 request before cancel, got %d", hits)
	}
}

func TestTTLCache_Set_UpdateMovesToFrontAndRenewsExpiry(t *testing.T) {
	ttl := 1 * time.Minute
	c := newTTLCache[int](ttl, 2)

	// deterministic clock
	base := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	c.now = func() time.Time { return base }

	// Fill with a (LRU after we touch b) and b (MRU)
	c.Set("a", 1)
	c.Set("b", 2)

	// Touch b via Get to make "a" the LRU at this moment
	if v, ok := c.Get("b"); !ok || v != 2 {
		t.Fatalf("warming b failed")
	}

	// Advance time near a's original expiry; then Set("a", 42) to update-in-place.
	// This should:
	//  - overwrite value to 42
	//  - refresh expires to now+ttl (i.e., base+59s+1m)
	//  - move "a" to front (MRU)
	c.now = func() time.Time { return base.Add(59 * time.Second) }
	c.Set("a", 42)

	// Insert c to force eviction of current LRU (should be "b" now if move-to-front worked)
	c.Set("c", 3)

	if _, ok := c.Get("b"); ok {
		t.Fatalf("expected b to be evicted after a moved to front")
	}
	// "a" should be present and updated
	if v, ok := c.Get("a"); !ok || v != 42 {
		t.Fatalf("expected a present with updated value=42; got %v, ok=%v", v, ok)
	}
	if v, ok := c.Get("c"); !ok || v != 3 {
		t.Fatalf("expected c present; got %v, ok=%v", v, ok)
	}

	// Now check that a's expiry was actually renewed:
	// If expiry hadn't moved, at base+90s it would be expired (old expiry was base+60s).
	// But because Set() refreshed at base+59s, new expiry is base+119s — so at base+90s it's still fresh.
	c.now = func() time.Time { return base.Add(90 * time.Second) }
	if v, ok := c.Get("a"); !ok || v != 42 {
		t.Fatalf("expected a to be fresh due to renewed expiry at base+90s; got %v ok=%v", v, ok)
	}
}

func TestRespCache_Resize_ShrinkEvictsImmediately(t *testing.T) {
	rc := newRespCache(3, 10*time.Second)
	// deterministic clock
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	rc.now = func() time.Time { return base }

	rc.Store("a", []byte("A"), nil) // LRU after we add b,c
	rc.Store("b", []byte("B"), nil)
	rc.Store("c", []byte("C"), nil) // MRU

	// Shrink to 1 -> should evict "a" then "b", keep "c"
	rc.Resize(1)

	if _, ok := rc.Get("a"); ok {
		t.Fatalf("a should have been evicted on shrink")
	}
	if _, ok := rc.Get("b"); ok {
		t.Fatalf("b should have been evicted on shrink")
	}
	if v, ok := rc.Get("c"); !ok || string(v) != "C" {
		t.Fatalf("c should remain; got %q ok=%v", v, ok)
	}

	// Also ensure table only has c
	if _, ok := rc.tab["a"]; ok || rc.ll.Len() != 1 {
		t.Fatalf("internal structures not consistent after shrink")
	}
}

func TestRespCache_StoreNegative_UpdateExistingMovesToFrontAndSetsNegUntil(t *testing.T) {
	rc := newRespCache(2, 10*time.Second)
	base := time.Date(2025, 2, 2, 10, 0, 0, 0, time.UTC)
	rc.now = func() time.Time { return base }

	// Fill with two; access order to make u the LRU
	rc.Store("x", []byte("X"), nil) // older
	rc.Store("u", []byte("U"), nil) // newer (MRU)
	if _, ok := rc.Get("x"); !ok {  // touch x -> x becomes MRU, u becomes LRU
		t.Fatalf("expected x present")
	}

	// StoreNegative on existing "u" should:
	// - set negUntil in the future
	// - move "u" to front (MRU)
	rc.StoreNegative("u", time.Hour)

	// Confirm negUntil is set
	meta, ok := rc.Meta("u")
	if !ok || meta.negUntil.IsZero() || !meta.negUntil.After(base) {
		t.Fatalf("negUntil not updated: %+v ok=%v", meta, ok)
	}

	// Inserting a third item should evict current LRU ("x") if "u" moved to front
	rc.Store("y", []byte("Y"), nil) // capacity 2 -> evict LRU
	if _, ok := rc.Get("x"); ok {
		t.Fatalf("x should be evicted if u moved to front on StoreNegative")
	}
	// negative entries cause Get() to miss while active
	if _, ok := rc.Get("u"); ok {
		t.Fatalf("u is negative-cached; Get should miss until negUntil")
	}
}

func TestExpiryFromHeaders_UsesExpiresAndFallsBack(t *testing.T) {
	now := time.Date(2025, 3, 3, 12, 0, 0, 0, time.UTC)
	defTTL := 5 * time.Minute

	// 1) Uses Expires when Cache-Control is absent
	h1 := make(http.Header)
	h1.Set("Expires", now.Add(90*time.Second).Format(http.TimeFormat))
	d1 := expiryFromHeaders(h1, defTTL, now)
	if d1 < 85*time.Second || d1 > 95*time.Second {
		t.Fatalf("Expires not honored; got %v", d1)
	}

	// 2) Past Expires -> fallback to defTTL
	h2 := make(http.Header)
	h2.Set("Expires", now.Add(-30*time.Second).Format(http.TimeFormat))
	d2 := expiryFromHeaders(h2, defTTL, now)
	if d2 != defTTL {
		t.Fatalf("past Expires should fallback to defTTL; got %v", d2)
	}

	// 3) Invalid Expires -> fallback to defTTL
	h3 := make(http.Header)
	h3.Set("Expires", "not-a-date")
	d3 := expiryFromHeaders(h3, defTTL, now)
	if d3 != defTTL {
		t.Fatalf("invalid Expires should fallback to defTTL; got %v", d3)
	}

	// 4) Cache-Control overrides Expires; ensure we still prefer CC
	h4 := make(http.Header)
	h4.Set("Cache-Control", "max-age=42")
	h4.Set("Expires", now.Add(300*time.Second).Format(http.TimeFormat)) // should be ignored
	d4 := expiryFromHeaders(h4, defTTL, now)
	if d4 != 42*time.Second {
		t.Fatalf("Cache-Control should win; got %v", d4)
	}

	// 5) no-store / no-cache => zero TTL
	h5 := make(http.Header)
	h5.Set("Cache-Control", "no-cache, max-age=999")
	if d := expiryFromHeaders(h5, defTTL, now); d != 0 {
		t.Fatalf("no-cache must return 0; got %v", d)
	}
}
