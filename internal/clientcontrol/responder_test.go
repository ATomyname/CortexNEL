package clientcontrol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestReplySelectsJoinOpcodeByVersion 验证按版本分叉的应答规则。
func TestReplySelectsJoinOpcodeByVersion(t *testing.T) {
	cases := []struct {
		name       string
		version    string
		request    uint16
		wantOpcode int // -1 表示不应答
	}{
		{"高版本认证就绪推 1799", "1.20.1", opAuthReadyModern, opJoinServerModern},
		{"低版本收到 261 不应答", "1.12.2", opAuthReadyModern, -1},
		{"517 恒用 1031 应答", "1.20.1", opAuthReadyLegacy, opJoinServerLegacy},
		{"低版本 517 用 1031 应答", "1.12.2", opAuthReadyLegacy, opJoinServerLegacy},
	}
	target := JoinTarget{Host: "127.0.0.1", Port: 25565, RoleName: "Steve"}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			target.MCVersion = c.version
			payload, _ := reply(c.request, nil, target)
			if c.wantOpcode < 0 {
				if payload != nil {
					t.Fatalf("期望不应答，实得 %x", payload)
				}
				return
			}
			if payload == nil {
				t.Fatal("期望应答，实得 nil")
			}
			if got := binary.LittleEndian.Uint16(payload); got != uint16(c.wantOpcode) {
				t.Fatalf("应答操作码 = %d，期望 %d", got, c.wantOpcode)
			}
		})
	}
}

// TestReplyHeartbeatCarriesLauncherIdentity 心跳应答必须逐字节等于参考实现。
func TestReplyHeartbeatCarriesLauncherIdentity(t *testing.T) {
	payload, kind := reply(opHeartbeat, nil, JoinTarget{})
	if kind != replyHeartbeat {
		t.Fatalf("kind = %q，期望 %q", kind, replyHeartbeat)
	}

	var want bytes.Buffer
	want.Write(le16(opHeartbeat))
	want.Write(le16(uint16(len(heartbeatIdentity))))
	want.WriteString(heartbeatIdentity)

	if !bytes.Equal(payload, want.Bytes()) {
		t.Fatalf("心跳应答字节不符\n实得 %x\n期望 %x", payload, want.Bytes())
	}
	if heartbeatIdentity != "i'am wpflauncher" {
		t.Fatalf("启动器标识被改写为 %q；客户端按字面量比对，不得修正拼写", heartbeatIdentity)
	}
}

// TestReplyFilterCheckShape 消息过滤应答 = false + 两个 int64 零值。
func TestReplyFilterCheckShape(t *testing.T) {
	payload, kind := reply(opMessageFilterCheck, nil, JoinTarget{})
	if kind != replyFilterCheck {
		t.Fatalf("kind = %q，期望 %q", kind, replyFilterCheck)
	}
	// 2（操作码）+ 1（bool）+ 8 + 8 = 19
	if len(payload) != 19 {
		t.Fatalf("载荷长度 = %d，期望 19：%x", len(payload), payload)
	}
	for i, b := range payload[2:] {
		if b != 0 {
			t.Fatalf("参数区第 %d 字节 = %d，期望 0", i, b)
		}
	}
}

// TestReplyEchoesPlayerMessageCheck opcode 18 必须原样回吐。
//
// 实测依据（docs/facts/NETEASE-PROTOCOL.md §10）：官方启动器对每一帧 18 都回一帧
// 同长度 18，313/532/330/560 四种载荷各 3 次请求对 3 次应答。回吐字节必须与请求逐字节相同。
func TestReplyEchoesPlayerMessageCheck(t *testing.T) {
	req := newFrame(opCheckPlayerMessage)
	req.putInt32(7)
	req.putString("玩家消息校验载荷")
	request := req.payload()

	got, kind := reply(opCheckPlayerMessage, request, JoinTarget{})
	if kind != replyPlayerMessageEcho {
		t.Fatalf("kind = %q，期望 %q", kind, replyPlayerMessageEcho)
	}
	if !bytes.Equal(got, request) {
		t.Fatalf("回吐字节不符\n实得 %x\n期望 %x", got, request)
	}
	if n := binary.LittleEndian.Uint16(got); n != opCheckPlayerMessage {
		t.Fatalf("回吐操作码 = %d，期望 %d", n, opCheckPlayerMessage)
	}
	if len(got) != len(request) {
		t.Fatalf("回吐长度 = %d，期望与请求一致 %d", len(got), len(request))
	}
}

// TestReplyPlayerMessageCheckRejectsOvershortPayload 连操作码都装不下的载荷不回吐。
func TestReplyPlayerMessageCheckRejectsOvershortPayload(t *testing.T) {
	if got, kind := reply(opCheckPlayerMessage, []byte{0x12}, JoinTarget{}); got != nil {
		t.Fatalf("期望不回吐，实得 %x（kind=%s）", got, kind)
	}
	if got, kind := reply(opCheckPlayerMessage, nil, JoinTarget{}); got != nil {
		t.Fatalf("期望不回吐，实得 %x（kind=%s）", got, kind)
	}
}

// TestReplySilentOpcodes 官方启动器实测不应答的操作码都不应答。
//
// 其中 530 是按实测判定的：整 600s 一发、仅 2 字节载荷，官方启动器从不回它，
// 连接连续健康存活 1 小时（docs/facts/NETEASE-PROTOCOL.md §10）。
func TestReplySilentOpcodes(t *testing.T) {
	for _, opcode := range []uint16{opKeepAlive, opPlayerSkin, opEntityCheck, opLoginGame} {
		if payload, kind := reply(opcode, nil, JoinTarget{}); payload != nil {
			t.Fatalf("操作码 %d 不应答，实得 %x（kind=%s）", opcode, payload, kind)
		}
	}
	if _, kind := reply(60000, nil, JoinTarget{}); kind != replyUnknownOpcode {
		t.Fatalf("未知操作码 kind = %q，期望 %q", kind, replyUnknownOpcode)
	}
}
