// 本地 AuthLib 鉴权代答协议监听（对应 Fantnel AuthLibProtocol 与官方客户端 @3 authlibWrapper）。
//
// 职责边界：
//  1. 监听 127.0.0.1 上分配的端口，并由 JVM 系统属性 -DlauncherControlPort 传给客户端；
//  2. 接收客户端组件在 joinServerWrapper 中发送的 3 段前缀字符串：
//     [int32 length][gameId][int32 length][userId][int32 length][serverHash]；
//  3. 将解析出的 serverHash 交由注入的 Joiner 闭包向网易 Yggdrasil 认证网关发起真实 join；
//  4. 返回 4 字节 int32 状态码（0 表示成功，1 表示失败）。
package authsock

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const loopbackHost = "127.0.0.1"

// Logger 最小日志能力。
type Logger interface {
	Info(module, message string, kv ...any)
	Warn(module, message string, kv ...any)
}

// JoinFunc 是 Yggdrasil join 动作的闭包签名。
// 入参为客户端发来的 serverHash（40-hex 字符串）；返回 error。
type JoinFunc func(ctx context.Context, gameID, userID, serverHash string) error

// Listener 管理鉴权端口的监听。
type Listener struct {
	inner  net.Listener
	join   JoinFunc
	log    Logger
	mu     sync.Mutex
	closed bool
}

// Listen 在本地环回开启鉴权通道监听。
func Listen(join JoinFunc, log Logger) (*Listener, error) {
	if join == nil {
		return nil, errors.New("authsock: join 函数不可为空")
	}
	if log == nil {
		return nil, errors.New("authsock: logger 不可为空")
	}

	inner, err := net.Listen("tcp", loopbackHost+":0")
	if err != nil {
		return nil, fmt.Errorf("authsock: 监听本地鉴权端口失败: %w", err)
	}

	l := &Listener{
		inner: inner,
		join:  join,
		log:   log,
	}

	go l.acceptLoop()
	log.Info("authsock", "本地 AuthLib 鉴权通道已开启", "port", l.Port())
	return l, nil
}

// Port 返回实际监听的端口。
func (l *Listener) Port() int {
	if addr, ok := l.inner.Addr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

// Close 关闭监听（幂等）。
func (l *Listener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	l.mu.Unlock()

	err := l.inner.Close()
	l.log.Info("authsock", "本地 AuthLib 鉴权通道已关闭")
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
				l.log.Warn("authsock", "鉴权通道停止接收连接", "err", err.Error())
			}
			return
		}
		l.log.Info("authsock", "收到客户端鉴权连接", "remote", conn.RemoteAddr().String())
		go l.serve(conn)
	}
}

// readLengthPrefixedString 读取 [int32 len] + [UTF-8 string]
func readLengthPrefixedString(r io.Reader) (string, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return "", err
	}
	length := int(binary.LittleEndian.Uint32(lenBuf[:]))
	if length < 0 || length > 4096 {
		return "", fmt.Errorf("authsock: 非法字符串长度 %d", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func (l *Listener) serve(conn net.Conn) {
	defer conn.Close()

	// 整个认证交互设置 30 秒超时
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	var responseCode uint32 = 1 // 默认 1 失败

	defer func() {
		var respBuf [4]byte
		binary.LittleEndian.PutUint32(respBuf[:], responseCode)
		if _, err := conn.Write(respBuf[:]); err != nil {
			l.log.Warn("authsock", "回写鉴权响应码失败", "err", err.Error())
		} else {
			l.log.Info("authsock", "已回写鉴权响应码", "code", responseCode)
		}
	}()

	gameID, err := readLengthPrefixedString(conn)
	if err != nil {
		l.log.Warn("authsock", "读取 gameID 失败", "err", err.Error())
		return
	}

	userID, err := readLengthPrefixedString(conn)
	if err != nil {
		l.log.Warn("authsock", "读取 userID 失败", "err", err.Error())
		return
	}

	serverHash, err := readLengthPrefixedString(conn)
	if err != nil {
		l.log.Warn("authsock", "读取 serverHash 失败", "err", err.Error())
		return
	}

	l.log.Info("authsock", "收到客户端 join 鉴权请求", "gameID", gameID, "userID", userID, "serverHash", serverHash)

	// 执行实际的 Yggdrasil join
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := l.join(ctx, gameID, userID, serverHash); err != nil {
		l.log.Warn("authsock", "Yggdrasil join 认证失败", "err", err.Error())
		responseCode = 1
		return
	}

	l.log.Info("authsock", "Yggdrasil join 认证成功")
	responseCode = 0
}
