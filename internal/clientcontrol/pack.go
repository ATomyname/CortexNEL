// 控制帧的值编码（真源 SimplePack.cs）。
// 全部为小端序：参考实现用 BitConverter，运行在 x86/x64 上即小端。
// 字符串是 uint16 小端字节长度 + UTF-8 内容；byte 切片直接写入且不带长度前缀。
package clientcontrol

import "encoding/binary"

// packer 按控制协议逐值追加字节。零值可用。
type packer struct {
	buf []byte
}

// newFrame 以操作码起头构造一帧载荷。
func newFrame(opcode uint16) *packer {
	p := &packer{buf: make([]byte, 0, 64)}
	p.putUint16(opcode)
	return p
}

func (p *packer) putBool(v bool) {
	var b byte
	if v {
		b = 1
	}
	p.buf = append(p.buf, b)
}

func (p *packer) putUint16(v uint16) {
	p.buf = binary.LittleEndian.AppendUint16(p.buf, v)
}

func (p *packer) putInt32(v int32) {
	p.buf = binary.LittleEndian.AppendUint32(p.buf, uint32(v))
}

func (p *packer) putInt64(v int64) {
	p.buf = binary.LittleEndian.AppendUint64(p.buf, uint64(v))
}

// putString 写 uint16 字节长度前缀 + UTF-8 内容。
// 超过 uint16 的字符串在本协议里不存在（角色名与地址都远小于上限），
// 真源同样直接截断为 ushort，这里保持一致而不额外报错。
func (p *packer) putString(v string) {
	raw := []byte(v)
	p.putUint16(uint16(len(raw)))
	p.buf = append(p.buf, raw...)
}

// payload 返回已编码的帧载荷（含起头的操作码）。
func (p *packer) payload() []byte {
	return p.buf
}
