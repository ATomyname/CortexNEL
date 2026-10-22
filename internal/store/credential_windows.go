//go:build windows

// DPAPI 凭据保护（DR-1/P5）：凭据只经本文件落盘，按当前用户加密。
package store

import (
	"encoding/base64"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	crypt32           = windows.NewLazySystemDLL("crypt32.dll")
	procProtectData   = crypt32.NewProc("CryptProtectData")
	procUnprotectData = crypt32.NewProc("CryptUnprotectData")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

// ProtectString 以 DPAPI（当前用户作用域）加密字符串，返回 base64 密文。
func ProtectString(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	in := dataBlob{cbData: uint32(len(plain)), pbData: &[]byte(plain)[0]}
	var out dataBlob
	r1, _, errNo := procProtectData.Call(
		uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0,
		windows.CRYPTPROTECT_UI_FORBIDDEN, uintptr(unsafe.Pointer(&out)),
	)
	if r1 == 0 {
		return "", fmt.Errorf("store: CryptProtectData 失败: %v", errNo)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.pbData)))
	cipher := unsafe.Slice(out.pbData, out.cbData)
	return base64.StdEncoding.EncodeToString(cipher), nil
}

// UnprotectString 解密 ProtectString 的产物。
func UnprotectString(cipherB64 string) (string, error) {
	if cipherB64 == "" {
		return "", nil
	}
	cipher, err := base64.StdEncoding.DecodeString(cipherB64)
	if err != nil {
		return "", err
	}
	in := dataBlob{cbData: uint32(len(cipher)), pbData: &cipher[0]}
	var out dataBlob
	r1, _, errNo := procUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0,
		windows.CRYPTPROTECT_UI_FORBIDDEN, uintptr(unsafe.Pointer(&out)),
	)
	if r1 == 0 {
		return "", fmt.Errorf("store: CryptUnprotectData 失败: %v", errNo)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.pbData)))
	plain := unsafe.Slice(out.pbData, out.cbData)
	return string(plain), nil
}
