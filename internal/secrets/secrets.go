package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const KeyLength = 32

type Encryptor struct {
	gcm cipher.AEAD
}

func NewEncryptor(base64Key string) (*Encryptor, error) {
	if base64Key == "" {
		return nil, errors.New("master key is empty")
	}

	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, fmt.Errorf("decoding master key: %w", err)
	}
	if len(key) != KeyLength {
		return nil, fmt.Errorf("master key must be %d bytes (got %d)", KeyLength, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("mreating AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}
	return &Encryptor{gcm: gcm}, nil
}

func (encryptor *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, encryptor.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	blob := encryptor.gcm.Seal(nonce, nonce, plaintext, nil)
	return blob, nil
}

func (encryptor *Encryptor) Decrypt(blob []byte) ([]byte, error) {
	nonceSize := encryptor.gcm.NonceSize()
	if len(blob) < nonceSize {
		return nil, errors.New("ciphertext too short to contain a nonce")
	}

	nonce, ciphertext := blob[:nonceSize], blob[nonceSize:]

	plaintext, err := encryptor.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (authentication): %w", err)
	}
	return plaintext, nil
}