//go:build !windows

// 非 Windows 平台桩：Nexus 以 Windows x64 为一等目标（规范 §2），
// 此桩仅保证跨平台编译通过，不提供凭据落盘能力。
package store

import "errors"

// ErrUnsupportedPlatform 表示当前平台不支持 DPAPI 凭据落盘。
var ErrUnsupportedPlatform = errors.New("store: 凭据保护仅支持 Windows（DPAPI）")

// ProtectString 在非 Windows 平台不可用。
func ProtectString(plain string) (string, error) { return "", ErrUnsupportedPlatform }

// UnprotectString 在非 Windows 平台不可用。
func UnprotectString(cipherB64 string) (string, error) { return "", ErrUnsupportedPlatform }
