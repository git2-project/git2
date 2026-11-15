// Package secretbox encrypts the instance secrets git2 keeps in its database,
// such as an OIDC client secret, with AES-256-GCM. The key comes from the
// environment (GIT2_SECRET_KEY), never from the database, so a database dump
// or backup alone does not reveal the secrets.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// KeySize is the key length in bytes: AES-256.
const KeySize = 32

// ErrDecrypt means the ciphertext was not sealed with this key and context,
// or was changed.
var ErrDecrypt = errors.New("secret could not be decrypted")

type Box struct {
	aead cipher.AEAD
}

// New takes the key as standard base64 of 32 random bytes, the form
// `openssl rand -base64 32` prints.
func New(keyBase64 string) (*Box, error) {
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return nil, fmt.Errorf("secret key is not base64: %w", err)
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("secret key must be %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext with a fresh random nonce and returns nonce and
// ciphertext together. context binds the result to where it is stored (for
// example "oidc_providers.client_secret:<id>"), so a sealed value copied to
// another row does not open there.
func (b *Box) Seal(plaintext []byte, context string) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize(), b.aead.NonceSize()+len(plaintext)+b.aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	return b.aead.Seal(nonce, nonce, plaintext, []byte(context)), nil
}

// Open reverses Seal with the same context. ErrDecrypt for a wrong key,
// context or a changed value.
func (b *Box) Open(sealed []byte, context string) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n+b.aead.Overhead() {
		return nil, ErrDecrypt
	}
	plain, err := b.aead.Open(nil, sealed[:n], sealed[n:], []byte(context))
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}
