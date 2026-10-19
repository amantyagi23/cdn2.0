package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

type Crypto struct {
	key []byte
}

func New(key string) (*Crypto, error) {
	keyBytes := []byte(key)

	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("encryption key must be exactly 32 bytes")
	}

	return &Crypto{
		key: keyBytes,
	}, nil
}

func (c *Crypto) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())

	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(
		nonce,
		nonce,
		[]byte(plaintext),
		nil,
	)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (c *Crypto) Decrypt(encrypted string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("decode encrypted data: %w", err)
	}

	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()

	if len(data) < nonceSize {
		return "", fmt.Errorf("invalid encrypted data")
	}

	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]

	plaintext, err := gcm.Open(
		nil,
		nonce,
		ciphertext,
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("decrypt data: %w", err)
	}

	return string(plaintext), nil
}
