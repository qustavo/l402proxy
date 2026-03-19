package lightning

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCLNBackend_Wait_NotSynced(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/getinfo" || r.Method != "POST" || r.Header.Get("Rune") != "test-rune" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"warning_bitcoind_sync":   "syncing blocks",
			"warning_lightningd_sync": "",
			"blockheight":             100,
		})
	}))
	defer server.Close()

	backend := &CLNBackend{
		baseURL: server.URL,
		rune:    "test-rune",
		client:  server.Client(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := backend.Wait(ctx)
	if err != context.DeadlineExceeded {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestCLNBackend_Wait_Synced(t *testing.T) {
	callCount := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/getinfo" || r.Method != "POST" || r.Header.Get("Rune") != "test-rune" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		callCount++
		if callCount < 2 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"warning_bitcoind_sync":   "",
				"warning_lightningd_sync": "syncing",
				"blockheight":             100,
			})
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"warning_bitcoind_sync":   "",
				"warning_lightningd_sync": "",
				"blockheight":             101,
			})
		}
	}))
	defer server.Close()

	backend := &CLNBackend{
		baseURL: server.URL,
		rune:    "test-rune",
		client:  server.Client(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := backend.Wait(ctx); err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if callCount < 2 {
		t.Errorf("expected at least 2 calls, got %d", callCount)
	}
}

func TestCLNBackend_CreateInvoice(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/invoice" || r.Method != "POST" || r.Header.Get("Rune") != "test-rune" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		if req["amount_msat"].(float64) != 5000 || req["description"].(string) != "test memo" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{
			"payment_hash": "abcd1234567890ef",
			"bolt11":       "lnbc50u1p...",
		})
	}))
	defer server.Close()

	backend := &CLNBackend{
		baseURL: server.URL,
		rune:    "test-rune",
		client:  server.Client(),
	}

	invoice, err := backend.CreateInvoice(context.Background(), 5000, "test memo")
	if err != nil {
		t.Fatalf("CreateInvoice failed: %v", err)
	}
	if invoice.PaymentHash != "abcd1234567890ef" {
		t.Errorf("expected payment_hash abcd1234567890ef, got %s", invoice.PaymentHash)
	}
	if invoice.PaymentRequest != "lnbc50u1p..." {
		t.Errorf("expected bolt11 lnbc50u1p..., got %s", invoice.PaymentRequest)
	}
	if invoice.AmountMsat != 5000 {
		t.Errorf("expected amount 5000, got %d", invoice.AmountMsat)
	}
}

func TestCLNBackend_VerifyPayment_Unpaid(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/listinvoices" || r.Method != "POST" || r.Header.Get("Rune") != "test-rune" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"invoices": []map[string]string{
				{"status": "unpaid"},
			},
		})
	}))
	defer server.Close()

	backend := &CLNBackend{
		baseURL: server.URL,
		rune:    "test-rune",
		client:  server.Client(),
	}

	paid, err := backend.VerifyPayment(context.Background(), "test-hash")
	if err != nil {
		t.Fatalf("VerifyPayment failed: %v", err)
	}
	if paid {
		t.Error("expected unpaid, got paid")
	}
}

func TestCLNBackend_VerifyPayment_Paid(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/listinvoices" || r.Method != "POST" || r.Header.Get("Rune") != "test-rune" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"invoices": []map[string]string{
				{"status": "paid"},
			},
		})
	}))
	defer server.Close()

	backend := &CLNBackend{
		baseURL: server.URL,
		rune:    "test-rune",
		client:  server.Client(),
	}

	paid, err := backend.VerifyPayment(context.Background(), "test-hash")
	if err != nil {
		t.Fatalf("VerifyPayment failed: %v", err)
	}
	if !paid {
		t.Error("expected paid, got unpaid")
	}
}

func TestCLNBackend_VerifyPayment_NotFound(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/listinvoices" || r.Method != "POST" || r.Header.Get("Rune") != "test-rune" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"invoices": []map[string]string{},
		})
	}))
	defer server.Close()

	backend := &CLNBackend{
		baseURL: server.URL,
		rune:    "test-rune",
		client:  server.Client(),
	}

	paid, err := backend.VerifyPayment(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("VerifyPayment failed: %v", err)
	}
	if paid {
		t.Error("expected not paid, got paid")
	}
}

func TestCLNBackend_HTTPError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Invalid rune"))
	}))
	defer server.Close()

	backend := &CLNBackend{
		baseURL: server.URL,
		rune:    "invalid-rune",
		client:  server.Client(),
	}

	_, err := backend.CreateInvoice(context.Background(), 5000, "test")
	if err == nil {
		t.Fatal("expected error for unauthorized request")
	}
}
