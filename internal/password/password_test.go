package password

import "testing"

func TestHashAndVerify(t *testing.T) {
	hash, err := Hash("correct horse")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if hash == "correct horse" {
		t.Fatal("hash equals the plain password")
	}
	if !Verify(hash, "correct horse") {
		t.Error("Verify rejected the right password")
	}
	if Verify(hash, "wrong horse") {
		t.Error("Verify accepted a wrong password")
	}
}
