package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// mockPayer implements Payer for testing.
type mockPayer struct {
	payCount int32
	preimage string
	payErr   error
	invoices []string
}

func (m *mockPayer) PayInvoice(ctx context.Context, bolt11 string) (string, error) {
	atomic.AddInt32(&m.payCount, 1)
	m.invoices = append(m.invoices, bolt11)
	if m.payErr != nil {
		return "", m.payErr
	}
	return m.preimage, nil
}

func TestL402RoundTripperWithoutCache(t *testing.T) {
	// Create a mock server that returns 402 on first request
	serverCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls++
		if serverCalls == 1 {
			// First call: return 402 with challenge
			token := "eyJpZCI6IjAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwIiwicGF5bWVudF9oYXNoIjoiYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhIiwiZXhwaXJlc19hdCI6OTk5OTk5OTk5OX0.deadbeef"
			invoice := "lnbc1000..."
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`L402 token="%s", invoice="%s"`, token, invoice))
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		// Second call: should have Authorization header
		auth := r.Header.Get("Authorization")
		if auth == "" {
			t.Fatal("expected Authorization header on second request")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"ok"}`)
	}))
	defer server.Close()

	payer := &mockPayer{preimage: "0011223344556677889900aabbccddee"}
	rt := New(payer)

	// Create a request
	req, err := http.NewRequest("GET", server.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}

	// First RoundTrip should trigger payment
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if atomic.LoadInt32(&payer.payCount) != 1 {
		t.Fatalf("expected 1 payment, got %d", atomic.LoadInt32(&payer.payCount))
	}

	if serverCalls != 2 {
		t.Fatalf("expected 2 server calls, got %d", serverCalls)
	}
}

func TestL402RoundTripperWithCache(t *testing.T) {
	serverCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls++
		if serverCalls == 1 {
			// First call: return 402 with challenge
			token := "eyJpZCI6IjAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwIiwicGF5bWVudF9oYXNoIjoiYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhIiwiZXhwaXJlc19hdCI6OTk5OTk5OTk5OX0.deadbeef"
			invoice := "lnbc1000..."
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`L402 token="%s", invoice="%s"`, token, invoice))
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		// Subsequent calls should have Authorization header
		auth := r.Header.Get("Authorization")
		if auth == "" {
			t.Fatal("expected Authorization header")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"ok"}`)
	}))
	defer server.Close()

	payer := &mockPayer{preimage: "0011223344556677889900aabbccddee"}
	store := NewMemoryTokenStore()
	rt := New(payer, WithStore(store))

	// First request
	req1, err := http.NewRequest("GET", server.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp1, err := rt.RoundTrip(req1)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp1.Body.Close()

	if atomic.LoadInt32(&payer.payCount) != 1 {
		t.Fatalf("expected 1 payment after first request, got %d", atomic.LoadInt32(&payer.payCount))
	}

	// Second request (should use cached token, no payment)
	req2, err := http.NewRequest("GET", server.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp2, err := rt.RoundTrip(req2)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()

	if atomic.LoadInt32(&payer.payCount) != 1 {
		t.Fatalf("expected still 1 payment (cached), got %d", atomic.LoadInt32(&payer.payCount))
	}

	if serverCalls != 3 {
		t.Fatalf("expected 3 server calls (1st: 402, 2nd: 200, 3rd: 200 with cached token), got %d", serverCalls)
	}
}

func TestCacheKeyDifferentPaths(t *testing.T) {
	serverCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls++
		if serverCalls%2 == 1 {
			// Odd calls: return 402
			token := "eyJpZCI6IjAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwIiwicGF5bWVudF9oYXNoIjoiYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhIiwiZXhwaXJlc19hdCI6OTk5OTk5OTk5OX0.deadbeef"
			invoice := "lnbc1000..."
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`L402 token="%s", invoice="%s"`, token, invoice))
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	payer := &mockPayer{preimage: "0011223344556677889900aabbccddee"}
	store := NewMemoryTokenStore()
	rt := New(payer, WithStore(store))

	// Request to /path1
	req1, _ := http.NewRequest("GET", server.URL+"/path1", nil)
	resp1, _ := rt.RoundTrip(req1)
	_ = resp1.Body.Close()

	// Request to /path2 (different path, should trigger separate payment)
	req2, _ := http.NewRequest("GET", server.URL+"/path2", nil)
	resp2, _ := rt.RoundTrip(req2)
	_ = resp2.Body.Close()

	if atomic.LoadInt32(&payer.payCount) != 2 {
		t.Fatalf("expected 2 payments (different paths), got %d", atomic.LoadInt32(&payer.payCount))
	}
}

func TestDecodeExpiry(t *testing.T) {
	testTime := time.Unix(1234567890, 0)
	payload := map[string]any{
		"id":           "test",
		"payment_hash": "test",
		"expires_at":   1234567890,
	}
	payloadJSON, _ := json.Marshal(payload)

	// Create a valid token format manually
	encodedPayload := base64.URLEncoding.EncodeToString(payloadJSON)
	token := encodedPayload + ".deadbeef"

	expiry, err := decodeExpiry(token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !expiry.Equal(testTime) {
		t.Fatalf("expected %v, got %v", testTime, expiry)
	}
}

func TestDecodeExpiryInvalidToken(t *testing.T) {
	// Invalid token format (no dot)
	token := "nodot"
	expiry, err := decodeExpiry(token)
	if err == nil {
		t.Fatal("expected error for invalid token format")
	}
	// Should return zero time
	if !expiry.IsZero() {
		t.Fatalf("expected zero time, got %v", expiry)
	}
}

func TestL402RoundTripperEvictInvalidToken(t *testing.T) {
	serverCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls++

		// Call 1: first request, no auth → 402
		if serverCalls == 1 {
			token := "eyJpZCI6IjAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwIiwicGF5bWVudF9oYXNoIjoiYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhIiwiZXhwaXJlc19hdCI6OTk5OTk5OTk5OX0.deadbeef"
			invoice := "lnbc1000..."
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`L402 token="%s", invoice="%s"`, token, invoice))
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		// Call 2: first request retry with auth → 200 success
		if serverCalls == 2 {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"status":"ok"}`)
			return
		}
		// Call 3: second request uses cached token with auth → 402 (token invalid on server)
		if serverCalls == 3 {
			token := "eyJpZCI6IjAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwIiwicGF5bWVudF9oYXNoIjoiYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhIiwiZXhwaXJlc19hdCI6OTk5OTk5OTk5OX0.deadbeef"
			invoice := "lnbc2000..."
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`L402 token="%s", invoice="%s"`, token, invoice))
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		// Call 4: second request re-attempt without auth (after token eviction) → 402
		if serverCalls == 4 {
			token := "eyJpZCI6IjAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwIiwicGF5bWVudF9oYXNoIjoiYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhIiwiZXhwaXJlc19hdCI6OTk5OTk5OTk5OX0.deadbeef"
			invoice := "lnbc3000..."
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`L402 token="%s", invoice="%s"`, token, invoice))
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		// Call 5: second request retry with new auth → 200 success
		if serverCalls == 5 {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"status":"ok"}`)
			return
		}
		t.Fatalf("unexpected call #%d", serverCalls)
	}))
	defer server.Close()

	payer := &mockPayer{preimage: "0011223344556677889900aabbccddee"}
	store := NewMemoryTokenStore()
	rt := New(payer, WithStore(store))

	// First request: no auth → 402 → pay → retry with auth → 200
	req1, _ := http.NewRequest("GET", server.URL+"/test", nil)
	resp1, _ := rt.RoundTrip(req1)
	_ = resp1.Body.Close()

	if atomic.LoadInt32(&payer.payCount) != 1 {
		t.Fatalf("expected 1 payment after first request, got %d", atomic.LoadInt32(&payer.payCount))
	}

	// Second request: cached token → 402 (invalid) → evict → no auth → 402 → pay → retry → 200
	req2, _ := http.NewRequest("GET", server.URL+"/test", nil)
	resp2, _ := rt.RoundTrip(req2)
	_ = resp2.Body.Close()

	if atomic.LoadInt32(&payer.payCount) != 2 {
		t.Fatalf("expected 2 payments (invalid cached token evicted and repaid), got %d", atomic.LoadInt32(&payer.payCount))
	}

	// Verify token was evicted from cache
	_, _, found := store.Get(server.URL + "/test")
	if found {
		t.Fatal("expected invalid token to be evicted from cache")
	}
}
