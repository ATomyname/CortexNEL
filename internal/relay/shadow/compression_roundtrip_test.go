// compression_roundtrip_test.go — 职责: 影子腿线上字节的自洽性验证。
//
// 用真实客户端（Netty）的解码口径反向校验我们发出去的字节：
// 帧 = [varint 帧长][varint dataLen][zlib(包ID+载荷) 或 原样]（dataLen=0）。
// 只要这三段任一处与协议不符，客户端就会 DecoderException。
//
// 为什么单独成文件：这是一次真实故障（2026-09-17，B 报
// Failed to decode packet 'clientbound/minecraft:game_profile'）的直接取证手段，
// 与业务语义无关，只校验字节。
package shadow

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"nexus/internal/relay/codec"
	"nexus/internal/relay/stage"
)

// readClientFrame 按 MC 客户端口径读一帧：返回 (帧长, dataLen, 解压后的包体)。
// 这是 Netty 解码器的等价实现，任一环节不符即返回错误。
func readClientFrame(conn net.Conn) (int, int, []byte, error) {
	var framer stage.Framer
	buf := make([]byte, 64*1024)
	for {
		body, err := framer.Next()
		if err == nil {
			// body = [dataLen varint][...]
			dataLen, n := binary.Uvarint(body)
			if n <= 0 {
				return 0, 0, nil, io.ErrUnexpectedEOF
			}
			rest := body[n:]
			if dataLen == 0 {
				// 未压缩：rest 就是包体
				return len(body), 0, rest, nil
			}
			zr, zerr := zlib.NewReader(bytes.NewReader(rest))
			if zerr != nil {
				return len(body), int(dataLen), nil, zerr
			}
			defer zr.Close()
			plain, rerr := io.ReadAll(io.LimitReader(zr, int64(dataLen)))
			if rerr != nil {
				return len(body), int(dataLen), nil, rerr
			}
			if len(plain) != int(dataLen) {
				return len(body), int(dataLen), nil, io.ErrShortBuffer
			}
			return len(body), int(dataLen), plain, nil
		}
		if err != stage.ErrNeedMore {
			return 0, 0, nil, err
		}
		n, rerr := conn.Read(buf)
		if n > 0 {
			framer.Push(buf[:n])
		}
		if rerr != nil {
			return 0, 0, nil, rerr
		}
	}
}

// TestShadowWireFormatIsClientDecodable 影子腿发出的字节必须能被
// "按帧长 → dataLen → zlib" 的标准客户端口径解出原始包体。
//
// 这是对 B 报 DecoderException 的直接取证：若本用例通过而真机仍失败，
// 说明问题不在字节格式，而在包语义（例如 B 缺少产出该包的 mod）。
func TestShadowWireFormatIsClientDecodable(t *testing.T) {
	// 复用同包的 rig：newRigConn 装配会话，attachShadowLeg 挂影子腿并返回 B 侧连接。
	c, _, _ := newRigConn(t)
	v, clientPeer := attachShadowLeg(t, c)

	const threshold int32 = 256
	v.SetCompression(threshold)

	type want struct {
		id      int32
		payload []byte
	}
	cases := []want{
		{0x2b, []byte("joingame-small")},         // 小于阈值 → dataLen=0
		{0x27, bytes.Repeat([]byte{0xAB}, 4096)}, // 大于阈值 → zlib
		{0x3e, []byte("player_info")},            // 小于阈值
	}

	// 并发读取：net.Pipe 无缓冲，写与读必须同时进行。
	type got struct {
		frameLen int
		dataLen  int
		body     []byte
		err      error
	}
	results := make(chan got, len(cases))
	go func() {
		for range cases {
			fl, dl, body, err := readClientFrame(clientPeer)
			results <- got{fl, dl, body, err}
		}
	}()

	for _, w := range cases {
		if err := v.WritePacket(w.id, w.payload); err != nil {
			t.Fatalf("WritePacket(%d): %v", w.id, err)
		}
	}

	for i, w := range cases {
		select {
		case g := <-results:
			if g.err != nil {
				t.Fatalf("第 %d 帧客户端口径解码失败（真机即 DecoderException）: %v", i, g.err)
			}
			// 解出的包体必须 = varint(包ID) + 载荷。
			r := codec.NewReader(g.body)
			id, err := r.VarInt()
			if err != nil {
				t.Fatalf("第 %d 帧包 ID 解析失败: %v", i, err)
			}
			if id != w.id {
				t.Fatalf("第 %d 帧包 ID = %d，期望 %d", i, id, w.id)
			}
			if !bytes.Equal(r.Rest(), w.payload) {
				t.Fatalf("第 %d 帧载荷不匹配", i)
			}
			// 大于阈值的帧必须是压缩帧（dataLen > 0）。
			if int32(len(g.body)) >= threshold && g.dataLen == 0 {
				t.Fatalf("第 %d 帧超过阈值却未压缩（dataLen=0）", i)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("第 %d 帧超时未收到", i)
		}
	}
}
