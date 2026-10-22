// Reader：从字节缓冲按 MC 协议（763）顺序读字段。全部方法无副作用共享底层切片。
package codec

import "unicode/utf8"

// Reader 顺序读取字段，pos 指示已消费偏移。
type Reader struct {
	buf []byte
	pos int
}

// NewReader 构造读取器。
func NewReader(buf []byte) *Reader {
	return &Reader{buf: buf}
}

// Remaining 返回尚未消费的字节数。
func (r *Reader) Remaining() int { return len(r.buf) - r.pos }

// Rest 以切片返回全部剩余字节（不消费检查）。
func (r *Reader) Rest() []byte { return r.buf[r.pos:] }

// Byte 读单字节。
func (r *Reader) Byte() (byte, error) {
	if r.Remaining() < 1 {
		return 0, ErrTruncated
	}
	b := r.buf[r.pos]
	r.pos++
	return b, nil
}

// Bool 读布尔。
func (r *Reader) Bool() (bool, error) {
	b, err := r.Byte()
	return b != 0, err
}

// UShort 读大端 uint16。
func (r *Reader) UShort() (uint16, error) {
	if r.Remaining() < 2 {
		return 0, ErrTruncated
	}
	v := uint16(r.buf[r.pos])<<8 | uint16(r.buf[r.pos+1])
	r.pos += 2
	return v, nil
}

// VarInt 读 varint（≤5B，facts §7.3）：LEB128，每字节低 7 位。
func (r *Reader) VarInt() (int32, error) {
	var result uint32
	for i := 0; i < 5; i++ {
		b, err := r.Byte()
		if err != nil {
			return 0, err
		}
		result |= uint32(b&0x7F) << (7 * i)
		if b&0x80 == 0 {
			return int32(result), nil
		}
	}
	return 0, ErrVarIntTooBig
}

// Fixed 读定长字节。
func (r *Reader) Fixed(n int) ([]byte, error) {
	if n < 0 || r.Remaining() < n {
		return nil, ErrTruncated
	}
	out := r.buf[r.pos : r.pos+n]
	r.pos += n
	return out, nil
}

// ByteArray 读 varint 长度前缀字节数组。
func (r *Reader) ByteArray() ([]byte, error) {
	n, err := r.VarInt()
	if err != nil {
		return nil, err
	}
	if n < 0 || n > MaxFramePayload {
		return nil, ErrStringTooLong
	}
	out, err := r.Fixed(int(n))
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), out...), nil
}

// String 读 varint 长度前缀 UTF-8 字符串。
func (r *Reader) String() (string, error) {
	n, err := r.VarInt()
	if err != nil {
		return "", err
	}
	if n < 0 || n > MaxStringLen {
		return "", ErrStringTooLong
	}
	raw, err := r.Fixed(int(n))
	if err != nil {
		return "", err
	}
	if !utf8.Valid(raw) {
		return "", ErrStringTooLong
	}
	return string(raw), nil
}
