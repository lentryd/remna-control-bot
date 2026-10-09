package settings

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const sealedPrefix = "v1:"

var errCannotOpen = errors.New("settings: cannot decrypt a stored secret")

// box encrypts stored secrets with a key derived from REF_SECRET, so no extra
// env variable is needed.
type box struct{ aead cipher.AEAD }

func newBox(secret string) (*box, error) {
	key, err := hkdf.Key(sha256.New, []byte(secret), []byte("vpn-control-bot/settings"), "secrets-v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &box{aead: aead}, nil
}

func (b *box) seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return sealedPrefix + base64.RawStdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (b *box) open(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	raw, ok := strings.CutPrefix(s, sealedPrefix)
	if !ok {
		return "", errCannotOpen
	}
	ct, err := base64.RawStdEncoding.DecodeString(raw)
	n := b.aead.NonceSize()
	if err != nil || len(ct) < n {
		return "", errCannotOpen
	}
	plain, err := b.aead.Open(nil, ct[:n], ct[n:], nil)
	if err != nil {
		return "", errCannotOpen
	}
	return string(plain), nil
}
