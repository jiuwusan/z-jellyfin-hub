package secret

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestBoxRejectsTamperingAndWrongContext(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	box, err := NewBox(key)
	if err != nil {
		t.Fatal(err)
	}
	one, err := box.Seal("credential:one:password", []byte("fixture-value"))
	if err != nil {
		t.Fatal(err)
	}
	two, _ := box.Seal("credential:one:password", []byte("fixture-value"))
	if bytes.Equal(one, two) {
		t.Fatal("nonce reused")
	}
	plain, err := box.Open("credential:one:password", one)
	if err != nil || string(plain) != "fixture-value" {
		t.Fatal("round trip failed")
	}
	if _, err = box.Open("credential:two:password", one); err == nil {
		t.Fatal("record substitution accepted")
	}
	one[len(one)-1] ^= 1
	if _, err = box.Open("credential:one:password", one); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	if _, err = NewBox([]byte("short")); err == nil {
		t.Fatal("short key accepted")
	}
}
func TestTokenAndPasswordVerification(t *testing.T) {
	token, err := Token()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := Digest(token)
	if err != nil || len(digest) != 32 {
		t.Fatal("invalid token digest")
	}
	if _, err = Digest("short"); err == nil {
		t.Fatal("invalid token accepted")
	}
	for _, invalid := range []string{token[:10] + "\n" + token[10:], token + "\r\n", " " + token} {
		if _, err = Digest(invalid); err == nil {
			t.Fatal("noncanonical token accepted")
		}
	}
	hash, err := HashPassword(token)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, token) || VerifyPassword(hash, token+"x") {
		t.Fatal("password verification failed")
	}
}
