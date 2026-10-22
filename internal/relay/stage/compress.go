// zlib 压缩 stage（架构文档 §6.2）：阈值协商前直通。
// 启用后每帧行内格式：[dataLen varint][dataLen>0 ? zlib(payload) : payload]。
package stage

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"io"
)

// ErrBadDataLen 表示帧内 dataLen 与实际数据不一致。
var ErrBadDataLen = errors.New("stage: 压缩帧 dataLen 非法")

// Compressor 是单腿的压缩上下文。
type Compressor struct {
	Threshold int32
}

// Decode 把帧体还原为载荷（读方向）：dataLen=0 直通，>0 走 zlib。
func (c *Compressor) Decode(body []byte) ([]byte, error) {
	if len(body) < 1 {
		return nil, ErrBadDataLen
	}
	dataLen, shift := binary.Uvarint(body)
	if shift <= 0 {
		return nil, ErrBadDataLen
	}
	pos := shift
	if dataLen == 0 {
		return body[pos:], nil
	}
	if dataLen > 4<<20 {
		return nil, ErrBadDataLen
	}
	zr, err := zlib.NewReader(bytes.NewReader(body[pos:]))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(io.LimitReader(zr, int64(dataLen)))
}

// Encode 把载荷编码为帧体（写方向）：超过阈值才压缩，否则 dataLen=0 直通。
func (c *Compressor) Encode(payload []byte) ([]byte, error) {
	if int32(len(payload)) < c.Threshold {
		out := make([]byte, 0, len(payload)+1)
		out = append(out, 0x00)
		out = append(out, payload...)
		return out, nil
	}
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(payload); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	out := make([]byte, 0, buf.Len()+5)
	out = appendVarInt(out, len(payload))
	out = append(out, buf.Bytes()...)
	return out, nil
}

func appendVarInt(dst []byte, v int) []byte {
	u := uint32(v)
	for {
		if u&^0x7F == 0 {
			return append(dst, byte(u))
		}
		dst = append(dst, byte(u&0x7F)|0x80)
		u >>= 7
	}
}
