// Writer：按 MC 协议（763）顺序写字段，底层缓冲自动增长。
package codec

import "encoding/binary"

// Writer 累积编码结果。
type Writer struct {
	buf []byte
}

// NewWriter 构造空写入器。
func NewWriter() *Writer { return &Writer{} }

// Bytes 返回已编码字节。
func (w *Writer) Bytes() []byte { return w.buf }

// Len 返回当前长度。
func (w *Writer) Len() int { return len(w.buf) }

// Byte 写单字节。
func (w *Writer) Byte(b byte) {
	w.buf = append(w.buf, b)
}

// Bool 写布尔。
func (w *Writer) Bool(v bool) {
	if v {
		w.Byte(1)
		return
	}
	w.Byte(0)
}

// UShort 写大端 uint16。
func (w *Writer) UShort(v uint16) {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	w.buf = append(w.buf, b[:]...)
}

// VarInt 写 varint。
func (w *Writer) VarInt(v int32) {
	u := uint32(v)
	for {
		if u&^0x7F == 0 {
			w.Byte(byte(u))
			return
		}
		w.Byte(byte(u&0x7F) | 0x80)
		u >>= 7
	}
}

// Fixed 写定长字节。
func (w *Writer) Fixed(b []byte) {
	w.buf = append(w.buf, b...)
}

// ByteArray 写 varint 长度前缀字节数组。
func (w *Writer) ByteArray(b []byte) {
	w.VarInt(int32(len(b)))
	w.Fixed(b)
}

// String 写 varint 长度前缀 UTF-8 字符串。
func (w *Writer) String(s string) {
	w.VarInt(int32(len(s)))
	w.buf = append(w.buf, s...)
}
