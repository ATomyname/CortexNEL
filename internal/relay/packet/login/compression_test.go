package login

import (
	"bytes"
	"testing"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

// TestEnableCompressionRoundTrip 压缩阈值必须按线上字节原样回写 —— 它同时驱动
// 两腿压缩切换，编码错一个字节会让 SetCompression 之后的所有帧错位。
// 线上字节直接手写，不复用被测的 Writer，避免自证。
func TestEnableCompressionRoundTrip(t *testing.T) {
	cases := []struct {
		threshold int32
		wire      []byte
	}{
		{0, []byte{0x00}},
		{256, []byte{0x80, 0x02}}, // 实测 domcer 上游用的就是 256
		{65536, []byte{0x80, 0x80, 0x04}},
	}
	for _, tc := range cases {
		c := relay.NewConn("test", nil, nil, testLogger(t))

		p := &enableCompression{}
		if err := p.Read(c, codec.NewReader(tc.wire)); err != nil {
			t.Fatalf("threshold=%d 解析失败: %v", tc.threshold, err)
		}
		if p.threshold != tc.threshold {
			t.Fatalf("threshold 读到 %d，期望 %d", p.threshold, tc.threshold)
		}

		w := codec.NewWriter()
		if err := p.Write(w); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(w.Bytes(), tc.wire) {
			t.Fatalf("threshold=%d 回写 = %x，期望线上字节 %x", tc.threshold, w.Bytes(), tc.wire)
		}
	}
}
