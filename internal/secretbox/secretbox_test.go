package secretbox

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func newKey(t *testing.T) string {
	t.Helper()
	k := make([]byte, KeySize)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(k)
}

func TestSealOpen(t *testing.T) {
	box, err := New(newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("client-secret-value")
	a, err := box.Seal(secret, "ctx:1")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := box.Seal(secret, "ctx:1")
	if bytes.Equal(a, b) {
		t.Error("two seals of the same value are equal: the nonce is not fresh")
	}
	if bytes.Contains(a, secret) {
		t.Error("sealed value contains the plaintext")
	}
	got, err := box.Open(a, "ctx:1")
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("Open = %q, %v", got, err)
	}

	other, _ := New(newKey(t))
	tampered := bytes.Clone(a)
	tampered[len(tampered)-1] ^= 1
	for name, open := range map[string]func() ([]byte, error){
		"other key":     func() ([]byte, error) { return other.Open(a, "ctx:1") },
		"other context": func() ([]byte, error) { return box.Open(a, "ctx:2") },
		"changed":       func() ([]byte, error) { return box.Open(tampered, "ctx:1") },
		"too short":     func() ([]byte, error) { return box.Open(a[:5], "ctx:1") },
	} {
		if _, err := open(); !errors.Is(err, ErrDecrypt) {
			t.Errorf("%s: err %v, want ErrDecrypt", name, err)
		}
	}
}

func TestNewRejectsBadKeys(t *testing.T) {
	for name, key := range map[string]string{
		"empty":     "",
		"not b64":   "not base64 !!",
		"too short": base64.StdEncoding.EncodeToString(make([]byte, 16)),
		"too long":  base64.StdEncoding.EncodeToString(make([]byte, 33)),
	} {
		if _, err := New(key); err == nil || !strings.Contains(err.Error(), "secret key") {
			t.Errorf("%s: err %v, want a secret key error", name, err)
		}
	}
}
