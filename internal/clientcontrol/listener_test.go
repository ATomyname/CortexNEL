package clientcontrol

import (
	"bytes"
	"encoding/binary"
	"net"
	"strconv"
	"testing"
	"time"
)

// stubLogger 收集日志调用，避免测试依赖 telemetry。
type stubLogger struct{}

func (stubLogger) Info(string, string, ...any) {}
func (stubLogger) Warn(string, string, ...any) {}

// TestListenerPushesJoinOnAuthReady 端到端最小回放：
// 假客户端回连控制端口并发出认证就绪帧，应收到带 relay 地址与角色名的进服指令。
func TestListenerPushesJoinOnAuthReady(t *testing.T) {
	target := JoinTarget{Host: "127.0.0.1", Port: 34567, RoleName: "张三", MCVersion: "1.20.1"}
	listener, err := Listen(target, stubLogger{})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	if listener.Port() <= 0 {
		t.Fatal("端口应大于 0，否则无法写入 launcherport")
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(listener.Port())), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}

	// 客户端发认证就绪（高版本）。
	if err := writeFrame(conn, newFrame(opAuthReadyModern).payload()); err != nil {
		t.Fatal(err)
	}

	frame, err := readFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	if got := frameOpcode(frame); got != opJoinServerModern {
		t.Fatalf("应答操作码 = %d，期望 %d", got, opJoinServerModern)
	}

	host, port, role := decodeJoinParams(t, frameParams(frame))
	if host != target.Host {
		t.Fatalf("推送主机 = %q，期望 %q", host, target.Host)
	}
	if port != target.Port {
		t.Fatalf("推送端口 = %d，期望 %d（relay 实际端口）", port, target.Port)
	}
	if role != target.RoleName {
		t.Fatalf("推送角色 = %q，期望 %q", role, target.RoleName)
	}
}

// TestListenerKeepsServingAfterSilentOpcode 未知/仅记录的操作码不得拆掉通道。
// 参考实现在这里会 break 掉收包循环，nexus 有意继续服务。
func TestListenerKeepsServingAfterSilentOpcode(t *testing.T) {
	listener, err := Listen(JoinTarget{Host: "127.0.0.1", Port: 25565, RoleName: "Steve", MCVersion: "1.20.1"}, stubLogger{})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(listener.Port())), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}

	// 先发一个不应答的操作码（530 保活，官方启动器实测也不应答），再发心跳；
	// 心跳仍应被应答。
	if err := writeFrame(conn, newFrame(opKeepAlive).payload()); err != nil {
		t.Fatal(err)
	}
	if err := writeFrame(conn, newFrame(opHeartbeat).payload()); err != nil {
		t.Fatal(err)
	}

	payload, err := readFrame(conn)
	if err != nil {
		t.Fatalf("通道在无需应答的操作码之后被拆掉：%v", err)
	}
	if got := frameOpcode(payload); got != opHeartbeat {
		t.Fatalf("应答操作码 = %d，期望 %d", got, opHeartbeat)
	}
	if got := decodeString(frameParams(payload)); got != heartbeatIdentity {
		t.Fatalf("心跳标识 = %q，期望 %q", got, heartbeatIdentity)
	}
}

// TestListenerEchoesPlayerMessageCheck opcode 18 在真实套接字上逐字节回吐。
// 旧实现把它当"仅记录"，官方启动器实测是逐包回等长同帧。
func TestListenerEchoesPlayerMessageCheck(t *testing.T) {
	listener, err := Listen(JoinTarget{Host: "127.0.0.1", Port: 25565, RoleName: "Steve", MCVersion: "1.20.1"}, stubLogger{})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(listener.Port())), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}

	request := newFrame(opCheckPlayerMessage)
	request.putInt32(3)
	request.putString("榜单文本")
	if err := writeFrame(conn, request.payload()); err != nil {
		t.Fatal(err)
	}

	payload, err := readFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	if got := frameOpcode(payload); got != opCheckPlayerMessage {
		t.Fatalf("应答操作码 = %d，期望 %d", got, opCheckPlayerMessage)
	}
	if !bytes.Equal(payload, request.payload()) {
		t.Fatalf("回吐字节不符\n实得 %x\n期望 %x", payload, request.payload())
	}
}

// TestListenRejectsIncompleteTarget 缺角色名或地址时拒绝开通道，
// 否则会推送一个客户端连不上的目标。
func TestListenRejectsIncompleteTarget(t *testing.T) {
	if _, err := Listen(JoinTarget{Host: "127.0.0.1", Port: 25565}, stubLogger{}); err == nil {
		t.Fatal("缺角色名应报错")
	}
	if _, err := Listen(JoinTarget{RoleName: "Steve"}, stubLogger{}); err == nil {
		t.Fatal("缺地址应报错")
	}
	if _, err := Listen(JoinTarget{Host: "127.0.0.1", Port: 25565, RoleName: "Steve"}, nil); err == nil {
		t.Fatal("缺日志器应报错")
	}
}

// TestCloseIsIdempotent 重复关闭不报错，便于会话清理路径无条件调用。
func TestCloseIsIdempotent(t *testing.T) {
	listener, err := Listen(JoinTarget{Host: "127.0.0.1", Port: 25565, RoleName: "Steve"}, stubLogger{})
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("二次关闭应为空操作：%v", err)
	}
}

func decodeJoinParams(t *testing.T, params []byte) (host string, port int, role string) {
	t.Helper()
	hostLen := int(binary.LittleEndian.Uint16(params))
	pos := 2 + hostLen
	host = string(params[2:pos])
	port = int(int32(binary.LittleEndian.Uint32(params[pos:])))
	pos += 4
	roleLen := int(binary.LittleEndian.Uint16(params[pos:]))
	pos += 2
	role = string(params[pos : pos+roleLen])
	return host, port, role
}

func decodeString(params []byte) string {
	n := int(binary.LittleEndian.Uint16(params))
	return string(params[2 : 2+n])
}
