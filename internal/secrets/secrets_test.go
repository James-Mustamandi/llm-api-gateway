package secrets

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"io"
	"testing"
)

func freshKey (t *testing.T) string {
	t.Helper()
	key := make([]byte, KeyLength)

	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		t.Fatalf("generating test key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func TestRoundTrip(t *testing.T) {
	encryptor, err := NewEncryptor(freshKey(t))
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	plaintext := []byte("sk-or-v1-shhhhsecretvendorkeydonttellanyone")

	blob, err := encryptor.Encrypt(plaintext)

	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	got, err := encryptor.Decrypt(blob)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, plaintext)
	}
}

func TestTamperingIsDetected(t *testing.T) {
	encryptor, _ := NewEncryptor(freshKey(t))
	blob, _ := encryptor.Encrypt([]byte("vendor-key"))

	tampered := bytes.Clone(blob)
	tampered[len(tampered)-1] ^= 0x01

	if _, err := encryptor.Decrypt(tampered); err == nil {
		t.Fatal("decryption succeeded on tampered ciphertext, integrity NOT protected")
	}

	tamperedNonce := bytes.Clone(blob)
	tamperedNonce[0] ^= 0x01

	if _, err := encryptor.Decrypt(tamperedNonce); err == nil {
		t.Fatal("decryption succeeded with tampered nonce integrity is NOT protected")
	}
}

func TestNonceIsUniquePerEncryption(t *testing.T) {
	encryptor, _ := NewEncryptor(freshKey(t))
	plaintext := []byte("identical-input")

	blob1, _ := encryptor.Encrypt(plaintext)
	blob2, _ := encryptor.Encrypt(plaintext)


	if bytes.Equal(blob1, blob2) {
		t.Fatal("Encrypting the same plaintext twice made an identical output, nonce is being used again which means broken GCM security")
	}
}

func TestWrongKeyFails(t *testing.T) {
	encryptor1, _ := NewEncryptor(freshKey(t))
	encryptor2, _ := NewEncryptor(freshKey(t))

	blob, _ := encryptor1.Encrypt([]byte("secret"))
	if _, err := encryptor2.Decrypt(blob); err == nil {
		t.Fatal("Decrypted with the wrong key, keys are not protecting anything")
	}

}

func TestRejectsBadMasterKey(t *testing.T) {
	cases :=map[string]string {
		"empty":		"",
		"not base64": 	"!!!not-base64!!!",
		"wrong length":	base64.StdEncoding.EncodeToString([]byte("too-short")),
	}

	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewEncryptor(key); err == nil {
				t.Errorf("NewEncryptor(%q) succeeded; want error", name)
			}
		})
	}
}