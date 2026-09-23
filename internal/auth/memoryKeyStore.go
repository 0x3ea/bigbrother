package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"sync"
)

var _ KeyStore = (*MemoryKeyStore)(nil)

type MemoryKeyStore struct {
	mu     sync.RWMutex
	hashes map[string][32]byte // name-> SHA-256
}

func NewMemoryKeyStore() *MemoryKeyStore {
	return &MemoryKeyStore{hashes: map[string][32]byte{}}
}

func (as *MemoryKeyStore) Issue(proberName string) string {
	raw := make([]byte, 32)
	rand.Read(raw)
	key := "bb_" + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(key))
	as.mu.Lock()
	defer as.mu.Unlock()
	as.hashes[proberName] = sum
	return key
}

func (as *MemoryKeyStore) Verify(proberName, key string) bool {
	sum := sha256.Sum256([]byte(key))

	as.mu.RLock()
	defer as.mu.RUnlock()
	stored := as.hashes[proberName]
	return subtle.ConstantTimeCompare(stored[:], sum[:]) == 1
}
