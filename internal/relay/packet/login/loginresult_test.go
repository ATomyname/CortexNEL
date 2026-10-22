package login

import (
	"bytes"
	"testing"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

// mcString 返回 MC 变长长度前缀的 UTF-8 字符串字节（facts §7.3）。
// 手工构造入参而不是用 codec.Writer，是为了让被测的 Read 真的面对线上字节，
// 避免「用同一个 Writer 造数据再断言它能读回来」的自证。
func mcString(s string) []byte {
	b := []byte(s)
	w := codec.NewWriter()
	w.VarInt(int32(len(b)))
	w.Fixed(b)
	return w.Bytes()
}

// TestLoginSuccessRoundTripsStringUUIDOnProtocol47 协议 47（1.8）的 LoginSuccess
// 是**字符串** UUID；按 16 字节二进制读会把 "xxxxxxxx-xxxx-4…" 截断，
// 随后 username 的 VarInt 长度从字符串中间取到垃圾值 → ErrTruncated，
// dispatch 据此直接关连接。
//
// 这是实测故障回放：protocol 47 / state=LOGIN / packetID=2 / dir=s2c /
// err=codec: 字节流提前结束（2026-09-22 13:19:54 日志）。
func TestLoginSuccessRoundTripsStringUUIDOnProtocol47(t *testing.T) {
	const wantUUID = "ce16b515-37de-4066-8a70-910018d2acda" // 36 字符，1.8 形态
	const wantName = "激怒的花豹"
	payload := append(mcString(wantUUID), mcString(wantName)...)

	c := relay.NewConn("test", nil, nil, testLogger(t))
	c.ProtocolVersion = 47

	p := &loginSuccess{}
	if err := p.Read(c, codec.NewReader(payload)); err != nil {
		t.Fatalf("协议 47 的 LoginSuccess 应能解析，实得 %v（修复前此处为 ErrTruncated）", err)
	}
	if p.username != wantName {
		t.Fatalf("username = %q，期望 %q", p.username, wantName)
	}
	if !p.uuidIsString {
		t.Fatal("协议 47 应判为字符串形态 UUID")
	}
	if got := string(p.uuidField); got != wantUUID {
		t.Fatalf("uuid = %q，期望 %q", got, wantUUID)
	}

	w := codec.NewWriter()
	if err := p.Write(w); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.Bytes(), payload) {
		t.Fatalf("回写字节与原包不一致\n入 = %x\n出 = %x", payload, w.Bytes())
	}

	// 1.8 没有 Configuration 态：LoginSuccess 直接入 Play，否则全部 play 处理器失效。
	if _, err := p.Handle(c); err != nil {
		t.Fatal(err)
	}
	if c.State() != relay.StatePlay {
		t.Fatalf("协议 47 登录后状态 = %v，期望 Play", c.State())
	}
}

// TestLoginSuccessRoundTripsBinaryUUID 1.16+（协议 735 起）UUID 是裸 16 字节，
// 其后是 username + properties + strict —— 后两项本实现不解释，走 RawForward 原样转发
// （比参考实现 sdk-ref/Codexus.ExampleMod/ProtocolSupport.cs 更容错：它把这两项
// 解析成强类型，遇到版本差异会直接失败）。
func TestLoginSuccessRoundTripsBinaryUUID(t *testing.T) {
	wantUUID := make([]byte, 16)
	for i := range wantUUID {
		wantUUID[i] = byte(i)
	}
	const wantName = "Steve"
	// 尾部模拟 properties 列表 + strict 布尔：按原样转发，不校验语义。
	tail := []byte{0x02, 0x04, 't', 'e', 's', 't', 0x04, 'v', 'a', 'l', 0x00, 0x01}
	payload := append(append(append([]byte{}, wantUUID...), mcString(wantName)...), tail...)

	c := relay.NewConn("test", nil, nil, testLogger(t))
	c.ProtocolVersion = 767

	p := &loginSuccess{}
	if err := p.Read(c, codec.NewReader(payload)); err != nil {
		t.Fatalf("二进制 UUID 形态解析失败: %v", err)
	}
	if p.uuidIsString {
		t.Fatal("协议 767 应判为二进制形态 UUID")
	}
	if !bytes.Equal(p.uuidField, wantUUID) {
		t.Fatalf("uuid 字节 = %x，期望 %x", p.uuidField, wantUUID)
	}
	if p.username != wantName {
		t.Fatalf("username = %q，期望 %q", p.username, wantName)
	}

	w := codec.NewWriter()
	if err := p.Write(w); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.Bytes(), payload) {
		t.Fatalf("回写字节与原包不一致\n入 = %x\n出 = %x", payload, w.Bytes())
	}

	// 1.20.2+（764 起）登录后先进 Configuration，再由 ack 转 Play。
	if _, err := p.Handle(c); err != nil {
		t.Fatal(err)
	}
	if c.State() != relay.StateConfiguration {
		t.Fatalf("协议 767 登录后状态 = %v，期望 Configuration", c.State())
	}
}

// TestLoginDisconnectRoundTrip 上游在登录阶段断开的原因必须原样回写，
// 它是用户能看到的唯一失败线索（「你还没有绑定网易账号」这类）。
func TestLoginDisconnectRoundTrip(t *testing.T) {
	const reason = "你还没有绑定网易账号"
	payload := mcString(reason)

	c := relay.NewConn("test", nil, nil, testLogger(t))
	p := &loginDisconnect{}
	if err := p.Read(c, codec.NewReader(payload)); err != nil {
		t.Fatalf("Disconnect 解析失败: %v", err)
	}
	if p.reason != reason {
		t.Fatalf("reason = %q，期望 %q", p.reason, reason)
	}

	w := codec.NewWriter()
	if err := p.Write(w); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.Bytes(), payload) {
		t.Fatalf("回写字节与原包不一致\n入 = %x\n出 = %x", payload, w.Bytes())
	}
	if cancel, err := p.Handle(c); err != nil || cancel {
		t.Fatalf("Handle: cancel=%v err=%v", cancel, err)
	}
}
