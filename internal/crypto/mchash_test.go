package crypto

import (
	"crypto/sha1"
	"testing"
)

func TestMinecraftAuthDigestVectors(t *testing.T) {
	// 1. Positive case (MSB 0)
	hNotch := sha1.Sum([]byte("Notch"))
	if got := FormatMinecraftDigest(hNotch[:]); got != "4ed1f46bbe04bc756bcb17c0c7ce3e4632f06a48" {
		t.Errorf("FormatMinecraftDigest(Notch) = %q, want %q", got, "4ed1f46bbe04bc756bcb17c0c7ce3e4632f06a48")
	}

	// 2. Negative case (MSB 1)
	hJeb := sha1.Sum([]byte("jeb_"))
	if got := FormatMinecraftDigest(hJeb[:]); got != "-7c9d5b0044c130109a5d7b5fb5c317c02b4e28c1" {
		t.Errorf("FormatMinecraftDigest(jeb_) = %q, want %q", got, "-7c9d5b0044c130109a5d7b5fb5c317c02b4e28c1")
	}

	// 3. Leading zero case
	raw := []byte{0x00, 0x0f, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa}
	if got := FormatMinecraftDigest(raw); got != "f123456789abcdef0112233445566778899aa" {
		t.Errorf("FormatMinecraftDigest(leading zero) = %q, want %q", got, "f123456789abcdef0112233445566778899aa")
	}
}
