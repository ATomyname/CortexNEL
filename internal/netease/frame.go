// Yggdrasil 帧编解码（facts §3.2/§3.4）：
// 外层 int16-LE 长度前缀帧；type-9 ChaCha 帧 = [len 明文][crc32][type][0x88×3][payload]，[2:] 整体加密。
package netease

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net"

	"nexus/internal/crypto"
)

// packChaChaFrame 构造 type-9 帧（Nirvana YggdrasilExtensions.PackMessage 同形）。
func packChaChaFrame(frameType byte, payload, key []byte) []byte {
	total := len(payload) + 10
	msg := make([]byte, total)
	binary.LittleEndian.PutUint16(msg[0:2], uint16(total-2))
	msg[6] = frameType
	msg[7], msg[8], msg[9] = 136, 136, 136
	copy(msg[10:], payload)
	binary.LittleEndian.PutUint32(msg[2:6], crc32.ChecksumIEEE(msg[6:]))
	cipher := crypto.NewChaCha8(key, ChaChaNonce, 0)
	cipher.XORKeyStream(msg[2:], msg[2:])
	return msg
}

// unpackChaChaFrame 解包 s2c 帧：解密后校验 crc32，返回 (type, payload)。
func unpackChaChaFrame(body, key []byte) (byte, []byte, error) {
	if len(body) < 8 {
		return 0, nil, errors.New("netease: 帧过短")
	}
	plain := append([]byte(nil), body...)
	cipher := crypto.NewChaCha8(key, ChaChaNonce, 0)
	cipher.XORKeyStream(plain, plain)
	want := crc32.ChecksumIEEE(plain[4:])
	got := binary.LittleEndian.Uint32(plain[0:4])
	if want != got {
		return 0, nil, fmt.Errorf("netease: 帧 CRC 校验失败 got=%08x want=%08x", got, want)
	}
	return plain[4], plain[8:], nil
}

// readLEFrame 读取 int16-LE 长度前缀帧体（facts §3.2：先读长度再读字节）。
func readLEFrame(conn net.Conn) ([]byte, error) {
	var head [2]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return nil, err
	}
	n := int(binary.LittleEndian.Uint16(head[:]))
	if n <= 0 {
		return nil, fmt.Errorf("netease: 帧长度非法 %d", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}
	return body, nil
}

// writeLEFrame 写 int16-LE 长度前缀帧体。
func writeLEFrame(conn net.Conn, body []byte) error {
	if len(body) > 0xFFFF {
		return errors.New("netease: 帧体超长")
	}
	head := make([]byte, 2)
	binary.LittleEndian.PutUint16(head, uint16(len(body)))
	if _, err := conn.Write(head); err != nil {
		return err
	}
	_, err := conn.Write(body)
	return err
}

// appendShortStringBE 写大端长度 shortStr（init 的盒子版本专用，facts §3.3）。
func appendShortStringBE(dst []byte, s string) []byte {
	dst = binary.BigEndian.AppendUint16(dst, uint16(len(s)))
	return append(dst, s...)
}

// appendShortStringLE 写小端长度 shortStr（join 的 modInfo/PrcCheck，facts §3.4）。
func appendShortStringLE(dst []byte, s string) []byte {
	dst = binary.LittleEndian.AppendUint16(dst, uint16(len(s)))
	return append(dst, s...)
}

func concat(a, b []byte) []byte {
	out := make([]byte, 0, len(a)+len(b))
	out = append(out, a...)
	return append(out, b...)
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
