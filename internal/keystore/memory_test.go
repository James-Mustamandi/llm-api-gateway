package keystore

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"testing"
	"github.com/James-Mustamandi/llm-api-gateway/internal/secrets"
)

func newTestStore(t *testing.T) *MemoryStore {
	t.Helper()
	key := make([]byte, 32)
	io.ReadFull(rand.Reader, key)
	encrypted, err := secrets.NewEncryptor(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	return NewMemoryStore(encrypted)
}

func TestSetGetRoundTrip(t *testing.T) {
	store := newTestStore(t)
	vendorKey := "sk-or-v1-secret"

	if err := store.Set("user_1", "openrouter", vendorKey); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := store.Get("user_1", "openrouter")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != vendorKey {
		t.Fatalf("got %q want %q", got, vendorKey)
	}
}

func TestStoredValueIsEncrypted(t *testing.T) {
	store := newTestStore(t)
	vendorKey := "this_should_not_appear"
	store.Set("user_1", "openrouter", vendorKey)

	blob := store.data["user_1"]["openrouter"]
	if bytes.Contains(blob, []byte(vendorKey)) {
		t.Fatal("stored blob contains plaintext vendor key, not ecrypted")
	}
}

func TestGetMissingReturnsNotFound(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Get("nobody", "openrouter"); !errors.Is(err, ErrorNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}

	store.Set("user_1", "openrouter", "k")
	if _, err := store.Get("user_1", "openai"); !errors.Is(err, ErrorNotFound) {
		t.Fatalf("got %v, want ErrNotFound for missing provider", err)
	}
}

func TestIsolationBetweenClients(t *testing.T) {
	store := newTestStore(t)
	store.Set("user_1", "openrouter", "user_1_key")
	store.Set("user_2", "openrouter", "user_2_key")

	user1, _ := store.Get("user_1", "openrouter")
	user2, _ := store.Get("user_2", "openrouter")

	if user1 == user2 {
		t.Fatal("clients share a vendor key... this should not happen")
	}
}
