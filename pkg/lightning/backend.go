package lightning

import "context"

// Backend is the interface any Lightning node implementation must satisfy.
type Backend interface {
	// Wait blocks until the node is fully synced to chain or ctx is cancelled.
	Wait(ctx context.Context) error
	CreateInvoice(ctx context.Context, amountMsat int64, memo string) (*Invoice, error)
	VerifyPayment(ctx context.Context, paymentHash string) (bool, error)
}

// Invoice holds the data returned after creating a payment request.
type Invoice struct {
	PaymentHash    string
	PaymentRequest string // BOLT-11
	AmountMsat     int64
}
