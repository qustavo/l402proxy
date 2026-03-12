package macaroon

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Token is the payload embedded in an issued access token.
type Token struct {
	ID          string `json:"id"`
	PaymentHash string `json:"payment_hash"`
	ExpiresAt   int64  `json:"expires_at"`
}

// Service issues and verifies stateless HMAC-signed tokens.
type Service struct {
	secret []byte
}

// NewService creates a Service backed by the given HMAC secret.
func NewService(secret []byte) *Service {
	return &Service{secret: secret}
}

// Issue creates a new signed token for the given payment hash, valid for ttl.
// Format: base64url(json_payload).hex(hmac_sha256)
func (s *Service) Issue(paymentHash string, ttl time.Duration) (string, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return "", fmt.Errorf("generating token id: %w", err)
	}

	t := Token{
		ID:          hex.EncodeToString(id),
		PaymentHash: paymentHash,
		ExpiresAt:   time.Now().Add(ttl).Unix(),
	}

	payload, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("marshaling token: %w", err)
	}

	encoded := base64.URLEncoding.EncodeToString(payload)
	sig := s.sign([]byte(encoded))
	return encoded + "." + hex.EncodeToString(sig), nil
}

// Verify parses and validates a token string, returning the embedded Token on success.
func (s *Service) Verify(raw string) (*Token, error) {
	parts := strings.SplitN(raw, ".", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid token format")
	}
	encoded, sigHex := parts[0], parts[1]

	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return nil, errors.New("invalid token signature encoding")
	}
	if !hmac.Equal(sig, s.sign([]byte(encoded))) {
		return nil, errors.New("invalid token signature")
	}

	payload, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("invalid token encoding")
	}

	var t Token
	if err := json.Unmarshal(payload, &t); err != nil {
		return nil, fmt.Errorf("invalid token payload: %w", err)
	}

	if time.Now().Unix() > t.ExpiresAt {
		return nil, errors.New("token expired")
	}

	return &t, nil
}

func (s *Service) sign(data []byte) []byte {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(data)
	return mac.Sum(nil)
}
