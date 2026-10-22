package crypto

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestECBAgainstFIPS197Vector(t *testing.T) {
	key := make([]byte, 16)
	plain := make([]byte, 16)
	// FIPS-197 附录 B：key=0、plain=0 的 AES-128 加密结果。
	want, _ := hex.DecodeString("66e94bd4ef8a2c3b884cfa59ca342b2e")
	got, err := ECBEncrypt(key, plain)
	if err != nil {
		t.Fatalf("ECBEncrypt: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ECBEncrypt = %x, want %x", got, want)
	}
	back, err := ECBDecrypt(key, got)
	if err != nil {
		t.Fatalf("ECBDecrypt: %v", err)
	}
	if !bytes.Equal(back, plain) {
		t.Fatalf("ECBDecrypt roundtrip = %x, want %x", back, plain)
	}
}

func TestECBRejectsBadBlockLength(t *testing.T) {
	if _, err := ECBEncrypt(make([]byte, 16), make([]byte, 15)); err == nil {
		t.Fatal("期望非 16B 倍数报错")
	}
	if _, err := ECBEncrypt(make([]byte, 16), nil); err == nil {
		t.Fatal("期望空输入报错")
	}
}

// TestECBPKCS7Roundtrip PKCS7 变体回环：任意长度（含整块边界）都应无损往返。
func TestECBPKCS7Roundtrip(t *testing.T) {
	key := make([]byte, 16)
	for _, n := range []int{1, 15, 16, 17, 32, 48} {
		plain := make([]byte, n)
		for i := range plain {
			plain[i] = byte(i)
		}
		enc, err := ECBEncryptPKCS7(key, plain)
		if err != nil {
			t.Fatalf("n=%d ECBEncryptPKCS7: %v", n, err)
		}
		wantLen := (n/16 + 1) * 16 // PKCS7：明文恰为整块时也补一整块。
		if len(enc) != wantLen {
			t.Fatalf("n=%d 密文长度 %d, 期望 %d", n, len(enc), wantLen)
		}
		back, err := ECBDecryptPKCS7(key, enc)
		if err != nil {
			t.Fatalf("n=%d ECBDecryptPKCS7: %v", n, err)
		}
		if !bytes.Equal(back, plain) {
			t.Fatalf("n=%d 往返不一致", n)
		}
	}
}

// TestECBDecryptPKCS7RejectsTampered 篡改填充字节必须报错，不得静默截断。
func TestECBDecryptPKCS7RejectsTampered(t *testing.T) {
	key := make([]byte, 16)
	enc, err := ECBEncryptPKCS7(key, []byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	enc[len(enc)-1] ^= 0xFF // 破坏填充长度字节
	if _, err := ECBDecryptPKCS7(key, enc); err == nil {
		t.Fatal("篡改填充应报错")
	}
	// 非法填充值（0）也应报错。
	enc2, _ := ECBEncryptPKCS7(key, []byte("abc"))
	enc2[len(enc2)-1] = 0
	if _, err := ECBDecryptPKCS7(key, enc2); err == nil {
		t.Fatal("填充值 0 应报错")
	}
}
