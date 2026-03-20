package client

import (
	"errors"
	"strings"
)

// parseChallenge parses a WWW-Authenticate header of the form:
// L402 token="<val>", invoice="<val>"
func parseChallenge(header string) (token, invoice string, err error) {
	after, ok := strings.CutPrefix(header, "L402 ")
	if !ok {
		return "", "", errors.New("not an L402 challenge")
	}

	// Parse token="..."
	tokenKey := "token="
	if !strings.HasPrefix(after, tokenKey) {
		return "", "", errors.New("missing token field")
	}
	after = after[len(tokenKey):]

	// Expect opening quote
	if !strings.HasPrefix(after, "\"") {
		return "", "", errors.New("malformed token value")
	}
	after = after[1:]

	// Find closing quote
	quoteIdx := strings.Index(after, "\"")
	if quoteIdx == -1 {
		return "", "", errors.New("malformed token value")
	}
	token = after[:quoteIdx]
	after = after[quoteIdx+1:]

	// Expect comma and space
	after = strings.TrimLeft(after, ", ")

	// Parse invoice="..."
	invoiceKey := "invoice="
	if !strings.HasPrefix(after, invoiceKey) {
		return "", "", errors.New("missing invoice field")
	}
	after = after[len(invoiceKey):]

	// Find opening and closing quotes
	if !strings.HasPrefix(after, "\"") {
		return "", "", errors.New("malformed invoice value")
	}
	after = after[1:]

	quoteIdx = strings.Index(after, "\"")
	if quoteIdx == -1 {
		return "", "", errors.New("malformed invoice value")
	}
	invoice = after[:quoteIdx]

	return token, invoice, nil
}
