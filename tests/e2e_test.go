package tests

import (
	"crypto/rand"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qustavo/l402proxy/pkg/client"
	"github.com/qustavo/l402proxy/pkg/lightning"
	"github.com/qustavo/l402proxy/pkg/proxy"
)

func TestL402Flow(t *testing.T) {
	lnd, err := lightning.NewLNDBackend(lightning.LNDConfig{
		Host:         "localhost:10009",
		CertPath:     rootPath("lnd/tls.cert"),
		MacaroonPath: rootPath("lnd/data/chain/bitcoin/regtest/admin.macaroon"),
	})
	if err != nil {
		t.Skipf("Could not create a LND backend (is nigiri running?): %v", err)
	}

	cln, err := lightning.NewCLNBackend(lightning.CLNConfig{
		BaseURL: "http://localhost:9835",
		Rune:    "1MOhfL2S_VjoqFKG-erWqmB9Kt5WU_MyNor6IMa65BQ9MA==",
	})
	if err != nil {
		t.Skipf("Could not create a CLN backend (is nigiri running?): %v", err)
	}

	t.Run("LND Proxy", func(t *testing.T) { testL402Flow(t, lnd, cln) })
	t.Run("CLN Proxy", func(t *testing.T) { testL402Flow(t, cln, lnd) })
}

func rootPath(s string) string {
	return filepath.Join(os.Getenv("HOME"), ".nigiri/volumes/", s)
}

// testL402Flow is the main test func. This tests the complete flow of a request:
// - starts the API backend (upstream).
// - starts the L402 Proxy.
// - Initiate a request; expect 402.
//   - Initiates a payment.
//   - Repeat the initial request and expects 200 (using client.Transport).
func testL402Flow(t *testing.T, ln lightning.Backend, payer client.Payer) {
	upstream := newTestUpstream(t)
	proxy := newTestProxy(t, ln, upstream.URL)

	var (
		c      = http.Client{}
		req, _ = http.NewRequest("GET", proxy.URL+"/test", nil)
	)

	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("Expected 402, got %d", resp.StatusCode)
	}

	// Now, attaching the L402 client transport should receive a 200, using the same request (req).
	// The transports manages the L402 payment flow.
	c.Transport = client.New(payer)
	resp, err = c.Do(req)
	if err != nil {
		t.Fatalf("HTTP Request with transport failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got: %d", resp.StatusCode)
	}
}

// Create upstream server that echoes back a test response.
func newTestUpstream(t *testing.T) *httptest.Server {
	fn := func(w http.ResponseWriter, r *http.Request) {
		// Verify Authorization header was stripped
		if r.Header.Get("Authorization") != "" {
			t.Fatal("Authorization header not stripped from upstream request")
		}

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
	}
	upstream := httptest.NewServer(http.HandlerFunc(fn))
	t.Cleanup(upstream.Close)

	return upstream
}

func newTestProxy(t *testing.T, ln lightning.Backend, upstream string) *httptest.Server {
	t.Log("Waiting for LND")
	if err := ln.Wait(t.Context()); err != nil {
		t.Skipf("Waiting for LND (is nigiri running?): %v", err)
	}

	srvURL, err := url.Parse(upstream)
	if err != nil {
		t.Fatal(err)
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}

	handler := proxy.New(proxy.Config{
		Upstream:    srvURL,
		PriceMsat:   10_000, // 10sats
		ServiceName: "test-service",
		TokenTTL:    1 * time.Minute,
	}, ln, secret, slog.Default())

	return httptest.NewServer(handler)
}
