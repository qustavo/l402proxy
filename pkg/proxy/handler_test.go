package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/qustavo/l402proxy/pkg/lightning"
	"github.com/qustavo/l402proxy/pkg/macaroon"
)

// testPreimage is a known 32-byte value; testPaymentHash is its SHA-256.
var (
	testPreimageBytes = make([]byte, 32) // all zeros
	testPaymentHash   = func() string {
		h := sha256.Sum256(testPreimageBytes)
		return hex.EncodeToString(h[:])
	}()
	testPreimageHex = hex.EncodeToString(testPreimageBytes)
)

// mockBackend is a test double for lightning.Backend.
type mockBackend struct {
	invoice *lightning.Invoice
	settled bool
}

func (m *mockBackend) Wait(_ context.Context) error { return nil }

func (m *mockBackend) CreateInvoice(_ context.Context, amountMsat int64, _ string) (*lightning.Invoice, error) {
	return m.invoice, nil
}

func (m *mockBackend) VerifyPayment(_ context.Context, _ string) (bool, error) {
	return m.settled, nil
}

func newTestHandler(t *testing.T, upstream *httptest.Server, settled bool) (*Handler, *macaroon.Service) {
	t.Helper()
	upstreamURL, _ := url.Parse(upstream.URL)
	secret := []byte("00000000000000000000000000000000")
	backend := &mockBackend{
		invoice: &lightning.Invoice{
			PaymentHash:    testPaymentHash,
			PaymentRequest: "lnbc10u1ptest",
			AmountMsat:     10_000,
		},
		settled: settled,
	}
	tokens := macaroon.NewService(secret)
	h := New(Config{
		Upstream:    upstreamURL,
		PriceMsat:   10_000,
		ServiceName: "test",
	}, backend, secret, slog.New(slog.NewTextHandler(io_discard{}, nil)))
	return h, tokens
}

// io_discard is an io.Writer that discards all output (for silencing test logs).
type io_discard struct{}

func (io_discard) Write(p []byte) (int, error) { return len(p), nil }

func TestServeHTTP_NoAuth_Returns402(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called")
	}))
	defer upstream.Close()

	h, _ := newTestHandler(t, upstream, false)
	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body["token"] == "" || body["invoice"] == "" {
		t.Errorf("expected token and invoice in body, got %v", body)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header")
	}
}

func TestServeHTTP_ValidAuth_Proxied(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		if r.Header.Get("Authorization") != "" {
			t.Error("Authorization header must be stripped before reaching upstream")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	h, tokens := newTestHandler(t, upstream, true)
	token, err := tokens.Issue(testPaymentHash, time.Hour)
	if err != nil {
		t.Fatalf("issuing token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	req.Header.Set("Authorization", "L402 "+token+":"+testPreimageHex)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !upstreamCalled {
		t.Error("upstream was not called")
	}
}

func TestServeHTTP_ValidToken_NotSettled_Returns402(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called")
	}))
	defer upstream.Close()

	h, tokens := newTestHandler(t, upstream, false) // not settled
	token, _ := tokens.Issue(testPaymentHash, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	req.Header.Set("Authorization", "L402 "+token+":"+testPreimageHex)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d", rec.Code)
	}
}

func TestServeHTTP_WrongPreimage_Returns402(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called")
	}))
	defer upstream.Close()

	h, tokens := newTestHandler(t, upstream, true)
	token, _ := tokens.Issue(testPaymentHash, time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	req.Header.Set("Authorization", "L402 "+token+":"+"deadbeef") // wrong preimage
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d", rec.Code)
	}
}

func TestParseAuth(t *testing.T) {
	cases := []struct {
		header   string
		token    string
		preimage string
		wantErr  bool
	}{
		{"L402 mytoken:mypreimage", "mytoken", "mypreimage", false},
		{"Bearer token", "", "", true},
		{"L402 nocolon", "", "", true},
		{"", "", "", true},
	}
	for _, tc := range cases {
		tok, pre, err := parseAuth(tc.header)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseAuth(%q): expected error, got nil", tc.header)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseAuth(%q): unexpected error: %v", tc.header, err)
			continue
		}
		if tok != tc.token || pre != tc.preimage {
			t.Errorf("parseAuth(%q): got (%s, %s), want (%s, %s)", tc.header, tok, pre, tc.token, tc.preimage)
		}
	}
}

func TestVerifyPreimage(t *testing.T) {
	if err := verifyPreimage(testPreimageHex, testPaymentHash); err != nil {
		t.Fatalf("valid preimage rejected: %v", err)
	}
	if err := verifyPreimage(testPreimageHex, "badhash"); err == nil {
		t.Fatal("expected error for wrong hash")
	}
	if err := verifyPreimage("nothex!!", testPaymentHash); err == nil {
		t.Fatal("expected error for invalid hex")
	}
}
