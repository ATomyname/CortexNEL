// Package codec 是 MC 协议（protocol 763）的帧级编解码原语（规范 I4：与使用者分离）。
// 常量与帧格式以 docs/facts/NETEASE-PROTOCOL.md §7.3 为准：varint 长度前缀、≤5B、单帧 ≤2MiB。
package codec

import "errors"

var (
	// ErrTruncated 表示字节流在读到完整字段前结束。
	ErrTruncated = errors.New("codec: 字节流提前结束")
	// ErrVarIntTooBig 表示 varint 超过 5 字节上限。
	ErrVarIntTooBig = errors.New("codec: varint 超过 5 字节")
	// ErrStringTooLong 表示字符串/字节数组超出保护上限。
	ErrStringTooLong = errors.New("codec: 长度超出保护上限")
)

// MaxStringLen 保护上限：MC 字符串协议上限 262144。
const MaxStringLen = 262144

// MaxFramePayload 单帧载荷保护上限（架构文档 §6.2 framer 规格）。
const MaxFramePayload = 2 * 1024 * 1024
