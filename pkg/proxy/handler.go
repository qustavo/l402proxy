package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/qustavo/l402proxy/pkg/lightning"
	"github.com/qustavo/l402proxy/pkg/macaroon"
)

// Config holds the proxy handler settings.
type Config struct {
	Upstream    *url.URL
	PriceMsat   int64
	ServiceName string
	TokenTTL    time.Duration
}

// Handler is an http.Handler that enforces L402 payment before proxying.
type Handler struct {
	cfg     Config
	proxy   *httputil.ReverseProxy
	backend lightning.Backend
	tokens  *macaroon.Service
	log     *slog.Logger
}

// New creates a Handler ready to serve requests.
func New(cfg Config, backend lightning.Backend, secret []byte, logger *slog.Logger) *Handler {
	return &Handler{
		cfg:     cfg,
		proxy:   httputil.NewSingleHostReverseProxy(cfg.Upstream),
		backend: backend,
		tokens:  macaroon.NewService(secret),
		log:     logger.With("module", "proxy"),
	}
}

// ServeHTTP implements the L402 flow:
//  1. If a valid L402 Authorization header is present → strip it, then proxy.
//  2. Otherwise → issue a 402 Payment Required challenge.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.log.Info(r.Method, "path", r.URL.Path)

	if auth := r.Header.Get("Authorization"); auth != "" {
		token, preimage, err := parseAuth(auth)
		if err != nil {
			h.log.Warn("invalid L402 credentials", "err", err, "path", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, `{"error":%q}`, err.Error())
			return
		}
		if err := h.validateAuth(r, token, preimage); err != nil {
			h.log.Debug("could not validate auth, continue with challenge", slog.Any("err", err))
		} else {
			// Strip the L402 header before forwarding so upstream never sees it.
			r = r.Clone(r.Context())
			r.Header.Del("Authorization")
			h.proxy.ServeHTTP(w, r)
			return
		}
	}

	if err := h.challenge(w, r); err != nil {
		h.log.Error("failed to issue payment challenge", "err", err)
		http.Error(w, "lightning backend unavailable: "+err.Error(), http.StatusServiceUnavailable)
	}
}

func (h *Handler) validateAuth(r *http.Request, rawToken, preimage string) error {
	tok, err := h.tokens.Verify(rawToken)
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}
	if err = verifyPreimage(preimage, tok.PaymentHash); err != nil {
		return fmt.Errorf("preimage: %w", err)
	}
	settled, err := h.backend.VerifyPayment(r.Context(), tok.PaymentHash)
	if err != nil {
		return fmt.Errorf("payment verification: %w", err)
	}
	if !settled {
		return errors.New("payment not settled")
	}
	return nil
}

func (h *Handler) challenge(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	memo := fmt.Sprintf("%s: %s %s", h.cfg.ServiceName, r.Method, r.URL.Path)
	inv, err := h.backend.CreateInvoice(ctx, h.cfg.PriceMsat, memo)
	if err != nil {
		return fmt.Errorf("creating invoice: %w", err)
	}

	token, err := h.tokens.Issue(inv.PaymentHash, h.cfg.TokenTTL)
	if err != nil {
		return fmt.Errorf("issuing token: %w", err)
	}

	h.log.Info("payment challenge issued",
		"payment_hash", inv.PaymentHash,
		"amount_msat", inv.AmountMsat,
		"path", r.URL.Path,
	)

	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`L402 token="%s", invoice="%s"`, token, inv.PaymentRequest))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	return json.NewEncoder(w).Encode(map[string]any{
		"token":       token,
		"invoice":     inv.PaymentRequest,
		"amount_msat": inv.AmountMsat,
	})
}

// parseAuth extracts token and preimage from an "L402 <token>:<preimage>" header.
func parseAuth(auth string) (token, preimage string, err error) {
	after, ok := strings.CutPrefix(auth, "L402 ")
	if !ok {
		return "", "", errors.New("not an L402 Authorization header")
	}
	token, preimage, ok = strings.Cut(after, ":")
	if !ok {
		return "", "", errors.New("L402 header missing ':' separator")
	}
	return token, preimage, nil
}

// verifyPreimage checks that sha256(preimage) == paymentHash.
func verifyPreimage(preimageHex, paymentHash string) error {
	preimage, err := hex.DecodeString(preimageHex)
	if err != nil {
		return fmt.Errorf("decoding preimage: %w", err)
	}
	digest := sha256.Sum256(preimage)
	if hex.EncodeToString(digest[:]) != paymentHash {
		return errors.New("preimage does not match payment hash")
	}
	return nil
}
