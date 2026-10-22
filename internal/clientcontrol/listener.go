// 环回控制端口的监听与连接生命周期。
package clientcontrol

import (
	"errors"
	"fmt"
	"net"
	"sync"
)

// loopbackHost 是控制端口的绑定地址。
// 只绑环回：这条通道没有任何认证，暴露到局域网等于让外部进程指挥本机客户端进服。
const loopbackHost = "127.0.0.1"

// preferredPort 是网易客户端约定的首选控制端口（真源 Tools.GetUnusedPort(11413)）。
const preferredPort = 11413

// Logger 是本包所需的最小日志能力，由调用方注入（避免反向依赖 telemetry）。
type Logger interface {
	Info(module, message string, kv ...any)
	Warn(module, message string, kv ...any)
}

// Listener 持有控制通道的监听套接字，并为每条客户端连接驱动收发循环。
//
// 一次会话只服务一个游戏客户端，但监听保持开启：客户端可能在重连时重新握手，
// 参考实现同样是「接受新连接即替换当前读写端」。
type Listener struct {
	inner  net.Listener
	target JoinTarget
	log    Logger

	mu     sync.Mutex
	closed bool
}

// Listen 在环回地址上开启控制通道。
// 首选端口被占用时回落到系统分配端口，实际端口用 Port() 取得，
// 必须把该端口写进启动参数的 launcherport 字段，客户端才知道回连何处。
func Listen(target JoinTarget, log Logger) (*Listener, error) {
	if log == nil {
		return nil, errors.New("clientcontrol: 需要注入日志器")
	}
	if target.RoleName == "" {
		return nil, errors.New("clientcontrol: 进服目标缺少角色名")
	}
	if target.Host == "" || target.Port <= 0 {
		return nil, fmt.Errorf("clientcontrol: 进服目标地址无效: %s:%d", target.Host, target.Port)
	}

	inner, err := net.Listen("tcp", fmt.Sprintf("%s:%d", loopbackHost, preferredPort))
	if err != nil {
		log.Warn("clientcontrol", "首选控制端口被占用，回落系统分配端口",
			"preferred", preferredPort, "err", err.Error())
		inner, err = net.Listen("tcp", loopbackHost+":0")
		if err != nil {
			return nil, fmt.Errorf("clientcontrol: 控制端口监听失败: %w", err)
		}
	}

	l := &Listener{inner: inner, target: target, log: log}
	go l.acceptLoop()
	log.Info("clientcontrol", "客户端控制通道已开启",
		"port", l.Port(), "target", fmt.Sprintf("%s:%d", target.Host, target.Port), "role", target.RoleName)
	return l, nil
}

// Port 返回实际监听端口。
func (l *Listener) Port() int {
	if addr, ok := l.inner.Addr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

// Close 关闭监听。已建立的连接会因读取失败自行退出。
func (l *Listener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	l.mu.Unlock()

	err := l.inner.Close()
	l.log.Info("clientcontrol", "客户端控制通道已关闭")
	return err
}

func (l *Listener) isClosed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed
}

func (l *Listener) acceptLoop() {
	for {
		conn, err := l.inner.Accept()
		if err != nil {
			if !l.isClosed() {
				l.log.Warn("clientcontrol", "控制通道停止接受连接", "err", err.Error())
			}
			return
		}
		l.log.Info("clientcontrol", "客户端已回连控制通道", "remote", conn.RemoteAddr().String())
		go l.serve(conn)
	}
}

// serve 驱动单条连接的收发循环。
//
// 与参考实现的一处有意差异：参考实现在任一回调抛异常后就 break 掉整个收包循环
// （它对操作码 18 的解包必然抛 TargetException，从而提前拆掉通道）。这里对无需应答
// 或不认识的操作码只记录并继续读，因为「不认识某个操作码」不是断开通道的理由。
//
// 但不能据此认定 opcode 18 无需应答 —— 官方启动器对每一帧 18 都回等长同帧
// （实测见 docs/facts/NETEASE-PROTOCOL.md §10），回吐在 reply 里完成。
func (l *Listener) serve(conn net.Conn) {
	defer conn.Close()

	for {
		payload, err := readFrame(conn)
		if err != nil {
			if !l.isClosed() {
				l.log.Info("clientcontrol", "控制连接结束", "err", err.Error())
			}
			return
		}

		opcode := frameOpcode(payload)
		replyPayload, kind := reply(opcode, payload, l.target)
		if replyPayload == nil {
			l.log.Info("clientcontrol", "收到控制帧", "opcode", opcode, "action", string(kind))
			continue
		}
		if err := writeFrame(conn, replyPayload); err != nil {
			l.log.Warn("clientcontrol", "控制帧回写失败", "opcode", opcode, "err", err.Error())
			return
		}
		l.log.Info("clientcontrol", "已应答控制帧", "opcode", opcode, "action", string(kind))
	}
}
