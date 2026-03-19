package lightning

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// CLNConfig holds connection parameters for a Core Lightning node.
type CLNConfig struct {
	BaseURL  string // e.g. "https://localhost:3010"
	Rune     string // CLN auth token
	CertPath string // optional; empty = system CAs
}

// CLNBackend implements Backend against a Core Lightning node via REST API.
type CLNBackend struct {
	baseURL string
	rune    string
	client  *http.Client
}

// NewCLNBackend connects to a CLN node and returns a ready Backend.
func NewCLNBackend(cfg CLNConfig) (*CLNBackend, error) {
	var tlsConfig *tls.Config

	if cfg.CertPath != "" {
		certBytes, err := os.ReadFile(cfg.CertPath)
		if err != nil {
			return nil, fmt.Errorf("reading TLS cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(certBytes) {
			return nil, fmt.Errorf("failed to add CLN cert to pool")
		}
		tlsConfig = &tls.Config{RootCAs: pool}
	} else {
		tlsConfig = &tls.Config{}
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	return &CLNBackend{
		baseURL: cfg.BaseURL,
		rune:    cfg.Rune,
		client:  client,
	}, nil
}

// post performs a POST request to a CLN REST endpoint with auth headers and error handling.
func (b *CLNBackend) post(ctx context.Context, path string, body any) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", b.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Rune", b.rune)

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("POST %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}

	return resp, nil
}

// Wait blocks until the CLN node is fully synced to chain or ctx is cancelled.
// It polls getinfo every 5 seconds and logs progress.
func (b *CLNBackend) Wait(ctx context.Context) error {
	for {
		resp, err := b.post(ctx, "/v1/getinfo", nil)
		if err != nil {
			return fmt.Errorf("getinfo: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var info struct {
			WarningBitcoindSync   string `json:"warning_bitcoind_sync"`
			WarningLightningdSync string `json:"warning_lightningd_sync"`
			BlockHeight           int64  `json:"blockheight"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			return fmt.Errorf("decoding getinfo: %w", err)
		}

		// Synced when both warnings are absent/empty
		if info.WarningBitcoindSync == "" && info.WarningLightningdSync == "" {
			return nil
		}

		slog.Info("waiting for CLN to sync to chain...",
			"block_height", info.BlockHeight,
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// CreateInvoice asks CLN to create a new invoice and returns its details.
func (b *CLNBackend) CreateInvoice(ctx context.Context, amountMsat int64, memo string) (*Invoice, error) {
	// Generate unique label: 16 random bytes, hex-encoded
	labelBytes := make([]byte, 16)
	if _, err := rand.Read(labelBytes); err != nil {
		return nil, fmt.Errorf("generating invoice label: %w", err)
	}
	label := hex.EncodeToString(labelBytes)

	reqBody := map[string]any{
		"amount_msat": amountMsat,
		"label":       label,
		"description": memo,
	}

	resp, err := b.post(ctx, "/v1/invoice", reqBody)
	if err != nil {
		return nil, fmt.Errorf("invoice: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var invResp struct {
		PaymentHash string `json:"payment_hash"`
		Bolt11      string `json:"bolt11"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&invResp); err != nil {
		return nil, fmt.Errorf("decoding invoice response: %w", err)
	}

	return &Invoice{
		PaymentHash:    invResp.PaymentHash,
		PaymentRequest: invResp.Bolt11,
		AmountMsat:     amountMsat,
	}, nil
}

// VerifyPayment returns true if the invoice identified by paymentHash has been settled.
func (b *CLNBackend) VerifyPayment(ctx context.Context, paymentHash string) (bool, error) {
	reqBody := map[string]string{
		"payment_hash": paymentHash,
	}

	resp, err := b.post(ctx, "/v1/listinvoices", reqBody)
	if err != nil {
		return false, fmt.Errorf("listinvoices: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var listResp struct {
		Invoices []struct {
			Status string `json:"status"`
		} `json:"invoices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return false, fmt.Errorf("decoding listinvoices response: %w", err)
	}

	if len(listResp.Invoices) == 0 {
		return false, nil
	}

	return listResp.Invoices[0].Status == "paid", nil
}
