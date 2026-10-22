//go:build windows

package store

import "testing"

// DPAPI roundtrip：凭据唯一落盘通道的可用性验证（P5/DR-1）。
func TestDPAPIRoundTrip(t *testing.T) {
	secret := "x19-token-值-0123456789"
	cipher, err := ProtectString(secret)
	if err != nil {
		t.Fatalf("ProtectString: %v", err)
	}
	if cipher == secret || cipher == "" {
		t.Fatal("密文不应等于明文")
	}
	plain, err := UnprotectString(cipher)
	if err != nil {
		t.Fatalf("UnprotectString: %v", err)
	}
	if plain != secret {
		t.Fatalf("roundtrip = %q", plain)
	}
}
