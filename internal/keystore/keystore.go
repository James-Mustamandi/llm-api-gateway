package keystore

import "errors"


var ErrorNotFound = errors.New("vendor Key not found")

type Store interface {
	// Gets the decrypted vendor key for (gatewayKey, provider)... note returns decrypted plaintext
	Get(gateway, provider string) (string, error)

	// Set encrypts and stores vendor key for (gatewayKey, provider)
	Set(gateway, provider, vendorKey string) error
}

