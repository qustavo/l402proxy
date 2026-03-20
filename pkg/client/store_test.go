package client

import (
	"testing"
	"time"
)

func TestMemoryTokenStore_SetAndGet(t *testing.T) {
	store := NewMemoryTokenStore()

	// Set a token
	store.Set("key1", "token1", "preimage1", time.Now().Add(1*time.Hour))

	// Get it back
	token, preimage, ok := store.Get("key1")
	if !ok {
		t.Fatal("expected to find token in store")
	}
	if token != "token1" || preimage != "preimage1" {
		t.Fatalf("got token=%q, preimage=%q, want token1, preimage1", token, preimage)
	}
}

func TestMemoryTokenStore_NotFound(t *testing.T) {
	store := NewMemoryTokenStore()

	// Try to get a non-existent key
	_, _, ok := store.Get("nonexistent")
	if ok {
		t.Fatal("expected not to find token in store")
	}
}

func TestMemoryTokenStore_ExpiredToken(t *testing.T) {
	store := NewMemoryTokenStore()

	// Set a token that expires in the past
	store.Set("expired", "token", "preimage", time.Now().Add(-1*time.Hour))

	// Try to get it (should be evicted)
	_, _, ok := store.Get("expired")
	if ok {
		t.Fatal("expected expired token to be evicted")
	}
}

func TestMemoryTokenStore_MultipleKeys(t *testing.T) {
	store := NewMemoryTokenStore()

	// Set multiple tokens
	store.Set("key1", "token1", "preimage1", time.Now().Add(1*time.Hour))
	store.Set("key2", "token2", "preimage2", time.Now().Add(1*time.Hour))
	store.Set("key3", "token3", "preimage3", time.Now().Add(-1*time.Hour)) // expired

	// Get them back
	token1, _, ok1 := store.Get("key1")
	if !ok1 || token1 != "token1" {
		t.Fatal("key1 not found or incorrect")
	}

	token2, _, ok2 := store.Get("key2")
	if !ok2 || token2 != "token2" {
		t.Fatal("key2 not found or incorrect")
	}

	// key3 should be expired
	_, _, ok3 := store.Get("key3")
	if ok3 {
		t.Fatal("key3 should be expired and evicted")
	}
}

func TestMemoryTokenStore_Delete(t *testing.T) {
	store := NewMemoryTokenStore()

	// Set a token
	store.Set("key", "token", "preimage", time.Now().Add(1*time.Hour))

	// Verify it exists
	_, _, ok := store.Get("key")
	if !ok {
		t.Fatal("expected token to exist")
	}

	// Delete it
	store.Delete("key")

	// Verify it's gone
	_, _, ok = store.Get("key")
	if ok {
		t.Fatal("expected token to be deleted")
	}

	// Delete non-existent key (should be no-op)
	store.Delete("nonexistent")
}
