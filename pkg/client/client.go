package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Payer handles invoice payment for L402 flows.
type Payer interface {
	PayInvoice(ctx context.Context, bolt11 string) (preimageHex string, err error)
}

// L402RoundTripper is an http.RoundTripper that handles L402 payment flows.
// It transparently pays invoices, caches tokens, and adds Authorization headers.
type L402RoundTripper struct {
	inner              http.RoundTripper
	payer              Payer
	store              TokenStore
	ignoreExistingAuth bool
}

// Option is a functional option for configuring L402RoundTripper.
type Option func(*L402RoundTripper)

// WithTransport sets a custom inner http.RoundTripper (defaults to http.DefaultTransport).
func WithTransport(t http.RoundTripper) Option {
	return func(r *L402RoundTripper) {
		r.inner = t
	}
}

// WithStore sets a custom TokenStore for caching tokens.
// If not provided, tokens are not cached (default behavior).
// Cache keys are derived from the request's host and path (query params are ignored).
// Tokens are evicted from the cache when they expire or rejected by the server.
// Pass NewMemoryTokenStore() for in-memory caching, or implement TokenStore for custom backends.
// Passing nil will disable caching.
func WithStore(s TokenStore) Option {
	return func(r *L402RoundTripper) {
		r.store = s
	}
}

// IgnoreExistingAuth causes L402RoundTripper to ignore any existing Authorization header
// on the request and proceed with the L402 payment flow. Useful when you want to force
// L402 authentication even if the request already carries other credentials.
func IgnoreExistingAuth() Option {
	return func(r *L402RoundTripper) {
		r.ignoreExistingAuth = true
	}
}

// New creates an L402RoundTripper.
// By default, tokens are not cached (uses internal no-op store).
// Pass WithStore(NewMemoryTokenStore()) to enable caching.
func New(payer Payer, opts ...Option) *L402RoundTripper {
	rt := &L402RoundTripper{
		inner:              http.DefaultTransport,
		payer:              payer,
		ignoreExistingAuth: false,
	}
	for _, opt := range opts {
		opt(rt)
	}

	// This allow passing a WithStore(nil) for better ergonomics.
	if rt.store == nil {
		rt.store = &noopTokenStore{}
	}

	return rt
}

// RoundTrip implements http.RoundTripper, handling L402 flows transparently.
func (r *L402RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Step 0: If the original request already has an Authorization header and we're not
	// ignoring it, bypass the L402 payment flow and forward the request as-is.
	// The server will validate whatever credentials are already present.
	// This allows reusing existing auth (e.g., from a previous L402 exchange or other scheme).
	if req.Header.Get("Authorization") != "" && !r.ignoreExistingAuth {
		return r.inner.RoundTrip(req)
	}

	cacheKey := r.cacheKey(req)
	cloned := req.Clone(req.Context())

	// Step 1: Check if we have a cached token and set it as an Auth header.
	if token, preimage, ok := r.store.Get(cacheKey); ok {
		cloned.Header.Set("Authorization", fmt.Sprintf("L402 %s:%s", token, preimage))
	}

	resp, err := r.inner.RoundTrip(cloned)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusPaymentRequired {
		return resp, nil
	}

	// At this stage we've got a 402. We close the body as we are returning a new response. Also the
	// cache key is evicted.
	_ = resp.Body.Close()
	r.store.Delete(cacheKey)

	// Step 4: Parse WWW-Authenticate header
	wwwAuth := resp.Header.Get("WWW-Authenticate")
	token, invoice, err := parseChallenge(wwwAuth)
	if err != nil {
		return nil, fmt.Errorf("parsing L402 challenge: %w", err)
	}

	// Step 5: Call payer to pay the invoice
	preimage, err := r.payer.PayInvoice(req.Context(), invoice)
	if err != nil {
		return nil, fmt.Errorf("paying invoice: %w", err)
	}

	// Step 6: Decode token expiry and determine TTL
	expiresAt, err := decodeExpiry(token)
	if err != nil {
		// On decode error, use a default 24h TTL
		expiresAt = time.Now().Add(24 * time.Hour)
	}

	// Step 7: Cache the token (or no-op if using default store)
	r.store.Set(cacheKey, token, preimage, expiresAt)

	// Step 8: Clone original request and add Authorization header
	cloned.Header.Set("Authorization", fmt.Sprintf("L402 %s:%s", token, preimage))

	// Step 9: Forward with payment credentials and return final response
	return r.inner.RoundTrip(cloned)
}

// cacheKey derives a cache key from the request (host + path, ignoring query params).
func (r *L402RoundTripper) cacheKey(req *http.Request) string {
	return req.URL.Host + req.URL.Path
}

// tokenPayload represents the JSON payload embedded in an L402 token.
type tokenPayload struct {
	ExpiresAt int64 `json:"expires_at"`
}

// decodeExpiry extracts the expires_at field from a token.
// Token format: base64url(json_payload).hex(hmac_sha256)
// On error, returns time.Now().Add(24h) as a fallback.
func decodeExpiry(rawToken string) (time.Time, error) {
	// Split on the first dot
	parts := strings.SplitN(rawToken, ".", 2)
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("invalid token format")
	}

	encoded := parts[0]

	// Base64url decode
	payload, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return time.Time{}, fmt.Errorf("decoding token: %w", err)
	}

	// JSON unmarshal
	var tp tokenPayload
	if err := json.Unmarshal(payload, &tp); err != nil {
		return time.Time{}, fmt.Errorf("unmarshaling token payload: %w", err)
	}

	return time.Unix(tp.ExpiresAt, 0), nil
}
