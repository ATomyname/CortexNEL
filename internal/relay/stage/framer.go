// Package stage 是编解码 stage（架构文档 §6.2）：
// framer（varint 长度前缀，≤5B、单帧 ≤2MiB）、cipher（AES-CFB8）、compress（zlib）。
// facts §7.3 红线：加密腿的长度前缀本身流经密钥流（逐字节解 varint），禁止 4B 大端前缀。
package stage

import (
	"errors"

	"nexus/internal/relay/codec"
)

// ErrNeedMore 表示缓冲中尚无完整帧。
var ErrNeedMore = errors.New("stage: 帧不完整，需要更多字节")

// Framer 是有状态的帧切分器：喂入解密后的字节流，吐出完整帧体（不含长度前缀）。
type Framer struct {
	buf []byte
}

// Push 追加新到的字节。
func (f *Framer) Push(data []byte) {
	f.buf = append(f.buf, data...)
}

// Next 提取下一帧体；缓冲不足时返回 ErrNeedMore。
func (f *Framer) Next() ([]byte, error) {
	r := codec.NewReader(f.buf)
	n, err := r.VarInt()
	if err != nil {
		if errors.Is(err, codec.ErrTruncated) {
			return nil, ErrNeedMore
		}
		return nil, err
	}
	if n < 0 || n > codec.MaxFramePayload {
		return nil, codec.ErrStringTooLong
	}
	if r.Remaining() < int(n) {
		return nil, ErrNeedMore
	}
	body, err := r.Fixed(int(n))
	if err != nil {
		return nil, err
	}
	frame := append([]byte(nil), body...)
	f.buf = r.Rest()
	return frame, nil
}

// WriteFrame 组帧：varint 长度前缀 + 帧体（发送侧，未加密）。
func WriteFrame(body []byte) []byte {
	w := codec.NewWriter()
	w.VarInt(int32(len(body)))
	w.Fixed(body)
	return w.Bytes()
}
