package keystore

import (
	"sync"
	"github.com/James-Mustamandi/llm-api-gateway/internal/secrets"
)

type MemoryStore struct {
	encryptor *secrets.Encryptor
	mutex sync.RWMutex
	data map[string]map[string][]byte // gatewayKey -> provider -> ciphertext blob
}

func NewMemoryStore(encryptor *secrets.Encryptor) *MemoryStore {
	return &MemoryStore{
		encryptor: encryptor,
		data: make(map[string]map[string][]byte),
	}

}

func (store *MemoryStore) Set(gatewayKey, provider, vendorKey string) error {
	blob, err := store.encryptor.Encrypt([]byte(vendorKey))
	if err != nil {
		return err
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()

	if store.data[gatewayKey] == nil {
		store.data[gatewayKey] = make(map[string][]byte)
	}
	store.data[gatewayKey][provider] = blob
	return nil
}

func (store *MemoryStore) Get(gateway, provider string) (string, error) {
	store.mutex.RLock()
	blob, hasError := store.data[gateway][provider]
	store.mutex.RUnlock()
	if !hasError {
		return "", ErrorNotFound
	}
	plaintext, err := store.encryptor.Decrypt(blob)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}