// 控制通道的分帧（真源 GameRpcService.SendControlData / ListenControlConnect）。
// 帧 = uint16 小端载荷长度 + 载荷；载荷首 2 字节是操作码。
package clientcontrol

import (
	"encoding/binary"
	"fmt"
	"io"
)

// maxFramePayload 是载荷长度上限：长度字段本身是 uint16。
const maxFramePayload = 0xFFFF

// writeFrame 写出一帧：长度前缀 + 载荷。
func writeFrame(w io.Writer, payload []byte) error {
	if len(payload) > maxFramePayload {
		return fmt.Errorf("clientcontrol: 载荷 %d 字节超出 uint16 长度上限", len(payload))
	}
	frame := make([]byte, 2+len(payload))
	binary.LittleEndian.PutUint16(frame, uint16(len(payload)))
	copy(frame[2:], payload)
	_, err := w.Write(frame)
	return err
}

// readFrame 读入一帧，返回**完整载荷**（含起头 2 字节操作码）。
// 需要原样回吐的应答直接复用该切片（见 reply）；需要操作码或参数区时用
// frameOpcode / frameParams 取值。
func readFrame(r io.Reader) ([]byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := int(binary.LittleEndian.Uint16(header[:]))
	if size < 2 {
		return nil, fmt.Errorf("clientcontrol: 帧长 %d 不足以容纳操作码", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// frameOpcode 取载荷起头的操作码（小端 uint16）。
// 载荷长度由 readFrame 保证 ≥2；本函数对越界入参返回 0 而不 panic。
func frameOpcode(payload []byte) uint16 {
	if len(payload) < 2 {
		return 0
	}
	return binary.LittleEndian.Uint16(payload)
}

// frameParams 取载荷中操作码之后的参数区（可能为空）。
func frameParams(payload []byte) []byte {
	if len(payload) < 2 {
		return nil
	}
	return payload[2:]
}
