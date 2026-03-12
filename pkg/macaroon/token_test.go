package macaroon

import (
	"strings"
	"testing"
	"time"
)

var testSecret = []byte("00000000000000000000000000000000")

func TestIssueAndVerify(t *testing.T) {
	svc := NewService(testSecret)
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	raw, err := svc.Issue(hash, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	tok, err := svc.Verify(raw)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if tok.PaymentHash != hash {
		t.Errorf("payment hash mismatch: got %s, want %s", tok.PaymentHash, hash)
	}
}

func TestVerify_WrongSecret(t *testing.T) {
	svc1 := NewService(testSecret)
	svc2 := NewService([]byte("11111111111111111111111111111111"))

	raw, _ := svc1.Issue("abc", time.Hour)
	if _, err := svc2.Verify(raw); err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
}

func TestVerify_Tampered(t *testing.T) {
	svc := NewService(testSecret)
	raw, _ := svc.Issue("abc", time.Hour)

	// Flip a character in the payload portion.
	parts := strings.SplitN(raw, ".", 2)
	b := []byte(parts[0])
	b[5] ^= 0xFF
	tampered := string(b) + "." + parts[1]

	if _, err := svc.Verify(tampered); err == nil {
		t.Fatal("expected error for tampered token, got nil")
	}
}

func TestVerify_Expired(t *testing.T) {
	svc := NewService(testSecret)
	raw, _ := svc.Issue("abc", -time.Second) // already expired

	if _, err := svc.Verify(raw); err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestVerify_InvalidFormat(t *testing.T) {
	svc := NewService(testSecret)
	cases := []string{"", "nodot", "a.b.c"}
	for _, tc := range cases {
		if _, err := svc.Verify(tc); err == nil {
			t.Errorf("expected error for %q, got nil", tc)
		}
	}
}
