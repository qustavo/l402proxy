package client

import (
	"sync"
	"time"
)

// TokenStore holds cached L402 tokens and preimages.
type TokenStore interface {
	// Get retrieves a cached token and preimage for the given key.
	// Returns (token, preimage, ok) where ok indicates if a valid entry exists.
	Get(key string) (token, preimage string, ok bool)

	// Set stores a token and preimage for the given key, expiring at expiresAt.
	Set(key string, token, preimage string, expiresAt time.Time)

	// Delete removes an entry from the cache. If the entry does not exist, it's a no-op.
	Delete(key string)
}

// noopTokenStore is a no-op implementation used when no explicit store is provided.
type noopTokenStore struct{}

func (n *noopTokenStore) Get(key string) (token, preimage string, ok bool) {
	return "", "", false
}

func (n *noopTokenStore) Set(key string, token, preimage string, expiresAt time.Time) {}

func (n *noopTokenStore) Delete(key string) {}

// MemoryTokenStore is an in-memory implementation of TokenStore with lazy expiry.
type MemoryTokenStore struct {
	mu    sync.RWMutex
	items map[string]*cacheEntry
}

type cacheEntry struct {
	token, preimage string
	expiresAt       time.Time
}

// NewMemoryTokenStore creates a new in-memory token store.
func NewMemoryTokenStore() *MemoryTokenStore {
	return &MemoryTokenStore{
		items: make(map[string]*cacheEntry),
	}
}

// Get retrieves a cached token and preimage. Evicts expired entries on read.
func (m *MemoryTokenStore) Get(key string) (token, preimage string, ok bool) {
	m.mu.RLock()
	entry, exists := m.items[key]
	m.mu.RUnlock()

	if !exists {
		return "", "", false
	}

	// Lazy expiry: check if entry has expired
	if time.Now().After(entry.expiresAt) {
		m.mu.Lock()
		delete(m.items, key)
		m.mu.Unlock()
		return "", "", false
	}

	return entry.token, entry.preimage, true
}

// Set stores a token and preimage under the given key.
func (m *MemoryTokenStore) Set(key string, token, preimage string, expiresAt time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[key] = &cacheEntry{
		token:     token,
		preimage:  preimage,
		expiresAt: expiresAt,
	}
}

// Delete removes an entry from the cache.
func (m *MemoryTokenStore) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, key)
}
