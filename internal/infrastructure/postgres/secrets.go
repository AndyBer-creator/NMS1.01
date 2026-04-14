package postgres

import (
	"NMS1/internal/config"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
)

const encryptedSecretPrefix = "enc:v1:"

type secretProtector struct {
	enabled bool
	aead    cipher.AEAD
}

func newSecretProtectorFromEnv() (*secretProtector, error) {
	secret := strings.TrimSpace(config.EnvOrFile("NMS_DB_ENCRYPTION_KEY"))
	if secret == "" {
		return &secretProtector{enabled: false}, nil
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("db encryption cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("db encryption gcm: %w", err)
	}
	return &secretProtector{enabled: true, aead: aead}, nil
}

func (p *secretProtector) splitSecretForStorage(raw string) (plain sql.NullString, enc sql.NullString, err error) {
	val := strings.TrimSpace(raw)
	if val == "" {
		return sql.NullString{}, sql.NullString{}, nil
	}
	if !p.enabled {
		return sql.NullString{String: val, Valid: true}, sql.NullString{}, nil
	}
	ciphertext, err := p.encrypt(val)
	if err != nil {
		return sql.NullString{}, sql.NullString{}, err
	}
	return sql.NullString{}, sql.NullString{String: ciphertext, Valid: true}, nil
}

func (p *secretProtector) mergeSecretFromStorage(plain string, enc sql.NullString) (string, error) {
	if enc.Valid && strings.TrimSpace(enc.String) != "" {
		return p.decrypt(enc.String)
	}
	return plain, nil
}

func (p *secretProtector) encrypt(plain string) (string, error) {
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("db encryption nonce: %w", err)
	}
	sealed := p.aead.Seal(nil, nonce, []byte(plain), nil)
	buf := append(nonce, sealed...)
	return encryptedSecretPrefix + base64.RawStdEncoding.EncodeToString(buf), nil
}

func (p *secretProtector) decrypt(encoded string) (string, error) {
	if !strings.HasPrefix(encoded, encryptedSecretPrefix) {
		return "", fmt.Errorf("db encryption: unsupported payload format")
	}
	payload := strings.TrimPrefix(encoded, encryptedSecretPrefix)
	raw, err := base64.RawStdEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("db encryption decode: %w", err)
	}
	nonceSize := p.aead.NonceSize()
	if len(raw) <= nonceSize {
		return "", fmt.Errorf("db encryption: payload too short")
	}
	nonce := raw[:nonceSize]
	ciphertext := raw[nonceSize:]
	plain, err := p.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("db encryption decrypt: %w", err)
	}
	return string(plain), nil
}
