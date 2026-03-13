package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/qustavo/l402proxy/pkg/lightning"
	"github.com/qustavo/l402proxy/pkg/proxy"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "l402proxy",
		Usage: "Drop an L402 payment gate in front of any HTTP service",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "upstream",
				Usage:    "URL of the backend service",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "price",
				Usage:    "Price per request (e.g. 10sat, 1000msat)",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "listen",
				Usage: "Address to listen on",
				Value: ":8080",
			},
			&cli.StringFlag{
				Name:  "lnd-host",
				Usage: "LND gRPC host:port",
				Value: "localhost:10009",
			},
			&cli.StringFlag{
				Name:  "lnd-macaroon",
				Usage: "Path to LND admin macaroon",
				Value: defaultMacaroonPath(),
			},
			&cli.StringFlag{
				Name:  "lnd-cert",
				Usage: "Path to LND TLS cert",
				Value: defaultCertPath(),
			},
			&cli.StringFlag{
				Name:  "service-name",
				Usage: "Label used in invoice memos",
				Value: "l402proxy",
			},
			&cli.StringFlag{
				Name:  "secret-key",
				Usage: "Hex-encoded 32-byte HMAC secret (auto-generated if omitted — tokens won't survive restarts)",
			},
			&cli.DurationFlag{
				Name:  "token-ttl",
				Usage: "Token expiration duration (e.g. 24h, 1h30m, 48h)",
				Value: 24 * time.Hour,
			},
		},
		Action: run,
	}

	if err := app.Run(os.Args); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(c *cli.Context) error {
	log := slog.Default()

	upstream, err := url.Parse(c.String("upstream"))
	if err != nil {
		return fmt.Errorf("invalid upstream URL: %w", err)
	}

	priceMsat, err := parsePrice(c.String("price"))
	if err != nil {
		return fmt.Errorf("invalid price: %w", err)
	}

	secret, err := resolveSecret(c.String("secret-key"), log)
	if err != nil {
		return err
	}

	backend, err := lightning.NewLNDBackend(lightning.LNDConfig{
		Host:         c.String("lnd-host"),
		CertPath:     c.String("lnd-cert"),
		MacaroonPath: c.String("lnd-macaroon"),
	})
	if err != nil {
		return fmt.Errorf("connecting to LND: %w", err)
	}
	log.Info("waiting for LND to be ready...")
	if err := backend.Wait(context.Background()); err != nil {
		return fmt.Errorf("LND not ready: %w", err)
	}
	log.Info("LND ready")

	h := proxy.New(proxy.Config{
		Upstream:    upstream,
		PriceMsat:   priceMsat,
		ServiceName: c.String("service-name"),
		TokenTTL:    c.Duration("token-ttl"),
	}, backend, secret, log)

	addr := c.String("listen")
	log.Info("l402proxy started", "listen", addr, "upstream", upstream, "price_msat", priceMsat)
	return http.ListenAndServe(addr, h)
}

// parsePrice converts "10sat" → 10000 msat, "1000msat" → 1000 msat.
func parsePrice(s string) (int64, error) {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasSuffix(s, "msat"):
		var v int64
		if _, err := fmt.Sscanf(strings.TrimSuffix(s, "msat"), "%d", &v); err != nil {
			return 0, fmt.Errorf("parsing msat value: %w", err)
		}
		return v, nil
	case strings.HasSuffix(s, "sat"):
		var v int64
		if _, err := fmt.Sscanf(strings.TrimSuffix(s, "sat"), "%d", &v); err != nil {
			return 0, fmt.Errorf("parsing sat value: %w", err)
		}
		return v * 1000, nil
	default:
		return 0, fmt.Errorf("price must end in 'sat' or 'msat', got %q", s)
	}
}

// resolveSecret returns the HMAC secret from the flag or generates a random one.
func resolveSecret(hexKey string, log *slog.Logger) ([]byte, error) {
	if hexKey == "" {
		log.Warn("--secret-key not set; generating ephemeral key — tokens will not survive restarts")
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generating secret key: %w", err)
		}
		return key, nil
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("decoding --secret-key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("--secret-key must be 32 bytes, got %d", len(key))
	}
	return key, nil
}

func defaultMacaroonPath() string {
	home, _ := os.UserHomeDir()
	return home + "/.lnd/data/chain/bitcoin/mainnet/admin.macaroon"
}

func defaultCertPath() string {
	home, _ := os.UserHomeDir()
	return home + "/.lnd/tls.cert"
}
