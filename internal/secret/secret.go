package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"golang.org/x/crypto/argon2"
	"strings"
)

var ErrInvalid = errors.New("invalid encrypted value or credential")

type Box struct{ aead cipher.AEAD }

func NewBox(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, ErrInvalid
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead}, nil
}
func (b *Box) Seal(context string, plain []byte) ([]byte, error) {
	return append([]byte{1}, b.aead.Seal(nil, nil, plain, []byte("z-jellyfin-hub:v1:"+context))...), nil
}
func (b *Box) Open(context string, sealed []byte) ([]byte, error) {
	if len(sealed) < 29 || sealed[0] != 1 {
		return nil, ErrInvalid
	}
	plain, err := b.aead.Open(nil, nil, sealed[1:], []byte("z-jellyfin-hub:v1:"+context))
	if err != nil {
		return nil, ErrInvalid
	}
	return plain, nil
}
func Token() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func Digest(token string) ([]byte, error) {
	if len(token) != 43 {
		return nil, ErrInvalid
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(b) != 32 || base64.RawURLEncoding.EncodeToString(b) != token {
		return nil, ErrInvalid
	}
	hash := sha256.Sum256(b)
	return hash[:], nil
}
func Matches(hash []byte, token string) bool {
	got, err := Digest(token)
	return err == nil && len(hash) == 32 && subtle.ConstantTimeCompare(hash, got) == 1
}
func HashPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 1024 {
		return "", ErrInvalid
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}
func VerifyPassword(encoded, password string) bool {
	if len(password) > 1024 {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=1" {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(parts[4])
	want, f := base64.RawStdEncoding.DecodeString(parts[5])
	if e != nil || f != nil || len(salt) != 16 || len(want) != 32 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32)
	return subtle.ConstantTimeCompare(want, got) == 1
}
