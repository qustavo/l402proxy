package lightning

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

// LNDConfig holds connection parameters for an LND node.
type LNDConfig struct {
	Host         string
	CertPath     string
	MacaroonPath string
}

// LNDBackend implements Backend against an LND node via gRPC.
type LNDBackend struct {
	client   lnrpc.LightningClient
	router   routerrpc.RouterClient
	macaroon string
	log      *slog.Logger
}

// NewLNDBackend connects to an LND node and returns a ready Backend.
func NewLNDBackend(cfg LNDConfig) (*LNDBackend, error) {
	certBytes, err := os.ReadFile(cfg.CertPath)
	if err != nil {
		return nil, fmt.Errorf("reading TLS cert: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certBytes) {
		return nil, fmt.Errorf("failed to add LND cert to pool")
	}

	macBytes, err := os.ReadFile(cfg.MacaroonPath)
	if err != nil {
		return nil, fmt.Errorf("reading macaroon: %w", err)
	}

	conn, err := grpc.NewClient(cfg.Host,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{RootCAs: pool})),
	)
	if err != nil {
		return nil, fmt.Errorf("connecting to LND at %s: %w", cfg.Host, err)
	}

	return &LNDBackend{
		client:   lnrpc.NewLightningClient(conn),
		router:   routerrpc.NewRouterClient(conn),
		macaroon: hex.EncodeToString(macBytes),
		log:      slog.With("module", "LND"),
	}, nil
}

func (b *LNDBackend) withMacaroon(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "macaroon", b.macaroon)
}

// Wait blocks until the LND node is fully synced to chain or ctx is cancelled.
// It polls GetInfo every 5 seconds and logs progress.
func (b *LNDBackend) Wait(ctx context.Context) error {
	for {
		info, err := b.client.GetInfo(b.withMacaroon(ctx), &lnrpc.GetInfoRequest{})
		if err != nil {
			return fmt.Errorf("GetInfo: %w", err)
		}
		if info.SyncedToChain {
			return nil
		}
		b.log.Info("waiting for LND to sync to chain...",
			"block_height", info.BlockHeight,
			"synced_to_chain", info.SyncedToChain,
		)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// CreateInvoice asks LND to create a new invoice and returns its details.
func (b *LNDBackend) CreateInvoice(ctx context.Context, amountMsat int64, memo string) (*Invoice, error) {
	resp, err := b.client.AddInvoice(b.withMacaroon(ctx), &lnrpc.Invoice{
		Memo:      memo,
		ValueMsat: amountMsat,
	})
	if err != nil {
		return nil, fmt.Errorf("AddInvoice: %w", err)
	}
	return &Invoice{
		PaymentHash:    hex.EncodeToString(resp.RHash),
		PaymentRequest: resp.PaymentRequest,
		AmountMsat:     amountMsat,
	}, nil
}

// VerifyPayment returns true if the invoice identified by paymentHash has been settled.
func (b *LNDBackend) VerifyPayment(ctx context.Context, paymentHash string) (bool, error) {
	hashBytes, err := hex.DecodeString(paymentHash)
	if err != nil {
		return false, fmt.Errorf("decoding payment hash: %w", err)
	}
	inv, err := b.client.LookupInvoice(b.withMacaroon(ctx), &lnrpc.PaymentHash{RHash: hashBytes})
	if err != nil {
		return false, fmt.Errorf("LookupInvoice: %w", err)
	}

	return (inv.State == lnrpc.Invoice_SETTLED), nil
}

func (b *LNDBackend) PayInvoice(ctx context.Context, bolt11 string) (string, error) {
	stream, err := b.router.SendPaymentV2(b.withMacaroon(ctx), &routerrpc.SendPaymentRequest{
		PaymentRequest: bolt11,
	})
	if err != nil {
		return "", fmt.Errorf("sending payment: %w", err)
	}

	for {
		payment, err := stream.Recv()
		if err != nil {
			return "", fmt.Errorf("recv from stream: %w", err)
		}

		switch payment.Status {
		case lnrpc.Payment_SUCCEEDED:
			return payment.PaymentPreimage, nil
		case lnrpc.Payment_FAILED:
			return "", fmt.Errorf("payment failed: %s", payment.FailureReason)
		default:
		}
	}
}
