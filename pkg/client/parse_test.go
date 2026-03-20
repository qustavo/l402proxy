package client

import (
	"testing"
)

func TestParseChallenge(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		wantToken  string
		wantInvoice string
		wantErr    bool
	}{
		{
			name:        "valid challenge",
			header:      `L402 token="abc123", invoice="lnbc1000..."`,
			wantToken:   "abc123",
			wantInvoice: "lnbc1000...",
			wantErr:     false,
		},
		{
			name:        "valid with spaces",
			header:      `L402 token="token_value", invoice="inv_value"`,
			wantToken:   "token_value",
			wantInvoice: "inv_value",
			wantErr:     false,
		},
		{
			name:     "not L402",
			header:   `Bearer abc123`,
			wantErr:  true,
		},
		{
			name:     "missing token field",
			header:   `L402 invoice="lnbc1000..."`,
			wantErr:  true,
		},
		{
			name:     "missing invoice field",
			header:   `L402 token="abc123"`,
			wantErr:  true,
		},
		{
			name:     "malformed token value",
			header:   `L402 token=abc123, invoice="lnbc1000..."`,
			wantErr:  true,
		},
		{
			name:     "malformed invoice value",
			header:   `L402 token="abc123", invoice=lnbc1000...`,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, invoice, err := parseChallenge(tt.header)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseChallenge() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if token != tt.wantToken {
					t.Fatalf("token = %q, want %q", token, tt.wantToken)
				}
				if invoice != tt.wantInvoice {
					t.Fatalf("invoice = %q, want %q", invoice, tt.wantInvoice)
				}
			}
		})
	}
}
