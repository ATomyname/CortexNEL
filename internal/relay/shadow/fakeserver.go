// fakeserver.go — 职责: 对影子端 B 伪造登录段（login → play 入口），然后交接。
//
// 为什么存在：B 中途接入时期待一个"从零开始的服务器"（握手→登录→进 play），
// 而影子端只有 A 的录制内容。本类型把 B 从握手推进到 play 态入口，
// 之后 B 的 S2C 供给交给 replayer（重放）→ LIVE 镜像（fanout）。
//
// 与 login 包（internal/relay/packet/login）的关系：那边是"relay 作为客户端"
// 代答上游的 EncryptionRequest（relay→上游方向）；本文件是"relay 作为服务器"
// 对 B 说话（relay→B 方向），角色相反，不可复用，必须另写。
//
// 形态开关（用户决策 2026-09-17：离线形态优先）：
//   - ShapeOffline：Handshake → LoginStart → LoginSuccess。不发 EncryptionRequest。
package shadow

import (
	"crypto/md5"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"nexus/internal/relay/codec"
	"nexus/internal/relay/stage"
)

// LoginShape 是对 B 伪造登录时使用的形态。
type LoginShape string

const (
	// ShapeOfflineB 是离线形态：不做加密协商，LoginStart 后直接 LoginSuccess。
	ShapeOfflineB LoginShape = "offline"
)

// 763 登录阶段包 ID（与 internal/relay/packet/login 同源，facts §7.2）。
// 本包协议中立（P1）：仅登录段的 ID 与版本无关（0.20.1 前后一致），
// play 段的版本知识（锚点/KeepAlive/CustomPayload）一律由调用方注入。
const (
	idHSLoginStart      = 0 // C→S
	idHSEncryptionReq   = 1 // S→C
	idHSEncryptionResp  = 1 // C→S
	idHSLoginSuccess    = 2 // S→C
	idHSEnableCompress  = 3 // S→C
	idHSLoginDisconnect = 0 // S→C

	// 状态查询（nextState=1）阶段的包 ID，两侧都是 0/1 —— 与登录段同号但语义不同，
	// 由握手里的 nextState 区分，故分开命名避免误用。
	idStatusResponse = 0 // S→C：StatusResponse(JSON string)
	idPingPong       = 1 // 双向：Ping(int64) / Pong(同值)
)

// FakeServerConfig 是影子登录的一次性输入。
type FakeServerConfig struct {
	// Shape 决定登录形态；默认离线。
	Shape LoginShape
	// ProtocolVersion 是 B 握手里带来的协议版本（决定后续 play 段注入方）。
	// 本包只透传给调用方，不解释。
	ProtocolVersion int32
	// Username 是 LoginSuccess 回给 B 的用户名。空则用 B 自报的名字。
	Username string
	// Compression >= 0 时发 EnableCompression(阈值) 并在本腿启用压缩；<0 不启用。
	Compression int32
	// HandshakeTimeout 是等 B 每一帧的最长等待。
	HandshakeTimeout time.Duration
}

// HandshakeInfo 是 B 握手包的观测结果（诊断与 play 段注入需要）。
type HandshakeInfo struct {
	ProtocolVersion int32
	Host            string
	Port            uint16
	NextState       int32
}

// FakeLoginResult 是登录段完成后的产出。
type FakeLoginResult struct {
	Handshake   HandshakeInfo
	LoginName   string // B 自报的用户名
	Effective   string // LoginSuccess 实际回给 B 的用户名
	Compression int32  // 本腿生效的压缩阈值；<0 未启用
}

// FakeServer 对一条已接受的 B 连接执行伪造登录。
// 登录完成后调用方拿到 (结果, 双向帧通道)，本类型即退场 —— 后续由
// replayer/fanout 以 Viewer 语义接管（本类型不持有连接）。
type FakeServer struct {
	cfg FakeServerConfig

	// B 腿的写状态（登录段内本类型是唯一写者）。
	mu   sync.Mutex
	comp *stage.Compressor // 发 EnableCompression 后才有
}

// NewFakeServer 构造。cfg 为零值时取默认（离线、不压缩、30s 超时）。
func NewFakeServer(cfg FakeServerConfig) *FakeServer {
	if cfg.Shape == "" {
		cfg.Shape = ShapeOfflineB
	}
	// Compression 的零值 0 恰好是合法阈值，而"未配置"的语义是不启用（<0），
	// 因此必须在构造期把零值翻译为 -1 —— 否则调用方没配压缩也会发阈值 0。
	if cfg.Compression == 0 {
		cfg.Compression = -1
	}
	if cfg.HandshakeTimeout <= 0 {
		cfg.HandshakeTimeout = 30 * time.Second
	}
	return &FakeServer{cfg: cfg}
}

// ServeLogin 走完登录段并返回观测结果。出错时连接已被关闭。
//
// 与 probe 的区别：probe 是独立装置（自己监听、观测、退出）；
// 本方法嵌入影子接入流程（B 已被影子监听 accept），失败即断开 B。
func (f *FakeServer) ServeLogin(conn net.Conn) (*FakeLoginResult, error) {
	// 1. 握手。
	hsBody, err := f.readFrame(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("shadow: 读 B 握手: %w", err)
	}
	hs, err := decodeShadowHandshake(hsBody)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("shadow: 解析 B 握手: %w", err)
	}
	if hs.NextState == 1 {
		// B 先做了 server list ping（客户端在多人游戏界面刷新服务器条目、
		// 显示 MOTD/延迟/在线人数时发的就是它）。
		//
		// 必须应答而不是断开：官方客户端"添加服务器→连接"的流程里，
		// 状态查询先于登录，被断开就表现为 java.net.SocketException:
		// Connection reset，B 永远走不到登录段（2026-09-17 实测）。
		// 应答后本连接结束，客户端会另开一条连接做真正的登录。
		if err := f.serveStatusPing(conn, hs); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("shadow: 应答 B 状态查询: %w", err)
		}
		_ = conn.Close()
		return nil, errStatusQueryAnswered
	}

	// 2. LoginStart。
	lsBody, err := f.readFrame(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("shadow: 读 B LoginStart: %w", err)
	}
	loginName, err := decodeShadowLoginStart(lsBody)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("shadow: 解析 B LoginStart: %w", err)
	}

	// 3. 按形态推进。
	effective := f.cfg.Username
	if effective == "" {
		effective = loginName
	}
	switch f.cfg.Shape {
	case ShapeOfflineB:
		// 离线形态：直接 LoginSuccess（无 EncryptionRequest）。
	default:
		_ = conn.Close()
		return nil, fmt.Errorf("shadow: 未知登录形态 %q", f.cfg.Shape)
	}

	// 4. 压缩协商（可选，必须在 LoginSuccess 之前：B 收到 LoginSuccess 后
	// 立即按新阈值理解帧格式）。
	if f.cfg.Compression >= 0 {
		w := codec.NewWriter()
		w.VarInt(idHSEnableCompress)
		w.VarInt(f.cfg.Compression)
		if err := f.writeFrame(conn, w.Bytes()); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("shadow: 发 EnableCompression: %w", err)
		}
		f.mu.Lock()
		f.comp = &stage.Compressor{Threshold: f.cfg.Compression}
		f.mu.Unlock()
	}

	// 5. LoginSuccess：UUID 用离线命名空间（nameUUIDFromBytes("OfflinePlayer:"+name)）。
	//
	// 字段布局按协议世代精确匹配（真源 minecraft-data 逐版本协议定义，
	// 并经 766 真机解码失败实证核对；少写一个字段客户端就会读越界并断开）：
	//   uuid                 735+ (1.16) 为 16 字节二进制
	//   username             始终为字符串
	//   properties[]         759+ (1.19) 起存在，VarInt 计数开头；离线会话写 0
	//   strictErrorHandling  仅 766~767 (1.20.5~1.21.2) 存在，768 起移除
	// 766 少 properties + strictErrorHandling 必然 DecoderException（game_profile）
	// —— 2026-09-17 影子端真机故障的完整根因（此前只补了 properties）。
	w := codec.NewWriter()
	w.VarInt(idHSLoginSuccess)
	w.Fixed(offlineUUID(effective))
	w.String(effective)
	if hs.ProtocolVersion >= 759 {
		w.VarInt(0) // properties 数组计数 = 0（离线会话无签名属性）
	}
	if hs.ProtocolVersion >= 766 && hs.ProtocolVersion <= 767 {
		w.Bool(false) // strictErrorHandling = false（766~767 专有字段）
	}
	if err := f.writeFrame(conn, w.Bytes()); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("shadow: 发 LoginSuccess: %w", err)
	}

	// 6. 等待 B 的 LoginAcknowledged（764+ 必经；C2S 包 ID 0x03，空载荷）。
	//    B 收到 LoginSuccess 后自发回它，然后进入 configuration 态等待 config
	//    数据。此处不收它的话，回灌的 config 首帧会和 LoginAck 在线上竞速，
	//    且 B 的 config 态解码器可能先读到未知包。
	if hs.ProtocolVersion >= 764 {
		if err := conn.SetReadDeadline(time.Now().Add(loginAckTimeout)); err != nil {
			return nil, fmt.Errorf("shadow: 设置 LoginAck 读限: %w", err)
		}
		// B 在收到 SetCompression 后上行也会压缩（MC 语义：SetCompression 之后
		// 的帧带压缩格式）。必须启用本侧入站解压，否则解出的"包 id"是
		// 压缩 dataLen 字节——实测曾误读为 id=0（dataLen=0 未压缩标记）。
		var readCipher *stage.CipherPair // 离线形态无加密
		decomp := &stage.Compressor{Threshold: f.cfg.Compression}
		body, err := readOneShadowFrameDecompressed(conn, readCipher, decomp)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("shadow: 读 LoginAcknowledged: %w", err)
		}
		id, _, err := splitShadowPacket(body)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("shadow: 解析 LoginAcknowledged: %w", err)
		}
		if id != idHSLoginAcknowledged {
			_ = conn.Close()
			return nil, fmt.Errorf("shadow: 期望 LoginAcknowledged(id=%d)，实得 id=%d", idHSLoginAcknowledged, id)
		}
	}

	return &FakeLoginResult{
		Handshake:   hs,
		LoginName:   loginName,
		Effective:   effective,
		Compression: f.cfg.Compression,
	}, nil
}

// idHSLoginAcknowledged 是登录态 C2S 的 LoginAcknowledged 包号（764+）。
const idHSLoginAcknowledged = 0x03

// loginAckTimeout 是等待 B 回 LoginAcknowledged 的上限。
const loginAckTimeout = 10 * time.Second

// errStatusQueryAnswered 表示本次连接只完成了一次状态查询（server list ping）
// 并已被正常应答；调用方据此区分「正常结束」与「失败」。
var errStatusQueryAnswered = errors.New("shadow: 状态查询已应答（非登录连接）")

// readOneShadowFrameDecompressed 读一帧并按需要解压（读 B 上行）。
//
// 与 readOneShadowFrame 的区别：B 在收到 SetCompression 后上行带压缩格式，
// 帧体 = [dataLen varint][dataLen>0 ? zlib(包ID+载荷) : 原样]。
// 不解压会把 dataLen 字节误当包 ID 读出——实测曾把 LoginAck 误判为 id=0。
func readOneShadowFrameDecompressed(conn net.Conn, cipher *stage.CipherPair, comp *stage.Compressor) ([]byte, error) {
	body, err := readOneShadowFrame(conn, cipher)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return body, nil
	}
	return comp.Decode(body)
}

// statusResponseJSON 是 server list ping 的响应体。
//
// 字段取自原版 StatusResponse：B 客户端只把它当普通服务器显示，
// 因此给出一个诚实（不夸大）的最小描述即可 —— 描述里点明这是影子端，
// 便于用户在服务器列表里分辨，也避免误以为是真实服务器。
const statusResponseJSON = `{"version":{"name":"CortexNEL Shadow","protocol":%d},` +
	`"players":{"max":0,"online":0},"description":{"text":"CortexNEL 影子端"}}`

// serveStatusPing 应答一次 server list ping。
//
// 协议：客户端发完 Handshake(nextState=1) 后发 StatusRequest(id=0)，
// 服务端回 StatusResponse(id=0, JSON string)，客户端再发 Ping(id=1,
// int64 payload)，服务端回同值 Pong(id=1)，随后客户端关闭连接。
// 这里按序应答；客户端提前关闭不算错误（它可能只取响应就关）。
func (f *FakeServer) serveStatusPing(conn net.Conn, hs HandshakeInfo) error {
	// 1. 读 StatusRequest。用与 Ping 相同的短超时：客户端可能只做握手就关，
	//    空等 HandshakeTimeout（30s）会占住同步的监听循环。
	if err := conn.SetReadDeadline(time.Now().Add(statusPingWait)); err != nil {
		return nil
	}
	if _, err := readOneShadowFrame(conn, nil); err != nil {
		return nil
	}

	// 2. 回 StatusResponse：字符串载荷 = JSON。
	w := codec.NewWriter()
	w.VarInt(idStatusResponse)
	w.String(fmt.Sprintf(statusResponseJSON, hs.ProtocolVersion))
	if err := f.writeFrame(conn, w.Bytes()); err != nil {
		return fmt.Errorf("发 StatusResponse: %w", err)
	}

	// 3. 读 Ping 并回 Pong（可选：客户端拿到 StatusResponse 后可以直接关闭，
	//    原版客户端也常在超时后才发 Ping）。
	//
	// 这里必须用**短超时**而不是 f.cfg.HandshakeTimeout（默认 30s）：
	// 客户端不发 Ping 时若空等 30 秒，本次连接会长时间占住影子监听
	// （Serve 是同步调用 handleB），期间新连接一律得不到服务。
	if err := conn.SetReadDeadline(time.Now().Add(statusPingWait)); err != nil {
		return nil
	}
	pingBody, err := readOneShadowFrame(conn, nil)
	if err != nil {
		return nil // 客户端未发 Ping：正常结束，不算失败
	}
	if _, payload, splitErr := splitShadowPacket(pingBody); splitErr == nil {
		pong := codec.NewWriter()
		pong.VarInt(idPingPong)
		pong.Fixed(payload)
		if err := f.writeFrame(conn, pong.Bytes()); err != nil {
			return fmt.Errorf("发 Pong: %w", err)
		}
	}
	return nil
}

// statusPingWait 是等待客户端 Ping 的最长时间。
//
// 取值依据：Ping 是客户端拿到 StatusResponse 后的立即行为（同一往返内），
// 正常延迟在毫秒级；2 秒足以覆盖慢网络，又不会让影子监听被空连接占住。
const statusPingWait = 2 * time.Second

// readFrame 读一帧（登录段恒明文）。
func (f *FakeServer) readFrame(conn net.Conn) ([]byte, error) {
	if err := conn.SetReadDeadline(time.Now().Add(f.cfg.HandshakeTimeout)); err != nil {
		return nil, err
	}
	return readOneShadowFrame(conn, nil)
}

// writeFrame 发一帧（登录段内本类型是唯一写者；压缩/加密在锁内按当前腿状态应用）。
func (f *FakeServer) writeFrame(conn net.Conn, body []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	withID := body
	if f.comp != nil {
		// 登录段的帧（EnableCompression/LoginSuccess）按新阈值编码：
		// MC 语义是"本包之后"生效，但原版服务端从 SetCompression 起的所有帧都带压缩。
		// 实测口径：EnableCompression 自身不压缩（B 还没理解格式），其后帧压缩。
		// 因此这里只在"已发过 EnableCompression 之后的帧"（LoginSuccess）压缩 ——
		// 调用序保证 comp 只在 LoginSuccess 前一帧被赋值。
		encoded, err := f.comp.Encode(withID)
		if err != nil {
			return err
		}
		withID = encoded
	}
	frame := stage.WriteFrame(withID)
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	_, err := conn.Write(frame)
	return err
}

// readOneShadowFrame 从 conn 读一帧（可选解密），返回不含长度前缀的帧体。
// 与 relay.readOneFrame 形态一致但不能复用：那是 leg 的方法，绑定 Conn。
func readOneShadowFrame(conn net.Conn, cipher *stage.CipherPair) ([]byte, error) {
	var framer stage.Framer
	buf := make([]byte, 16*1024)
	for {
		body, err := framer.Next()
		if err == nil {
			return body, nil
		}
		if err != stage.ErrNeedMore {
			return nil, err
		}
		n, rerr := conn.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if cipher != nil {
				cipher.TransformRead(chunk)
			}
			framer.Push(chunk)
		}
		if rerr != nil {
			return nil, rerr
		}
	}
}

// decodeShadowHandshake 解析 B→影子 的握手帧。
func decodeShadowHandshake(body []byte) (HandshakeInfo, error) {
	id, payload, err := splitShadowPacket(body)
	if err != nil {
		return HandshakeInfo{}, err
	}
	if id != 0 {
		return HandshakeInfo{}, fmt.Errorf("期望握手(id=0)，实得 id=%d", id)
	}
	r := codec.NewReader(payload)
	var h HandshakeInfo
	if h.ProtocolVersion, err = r.VarInt(); err != nil {
		return h, err
	}
	if h.Host, err = r.String(); err != nil {
		return h, err
	}
	if h.Port, err = r.UShort(); err != nil {
		return h, err
	}
	if h.NextState, err = r.VarInt(); err != nil {
		return h, err
	}
	return h, nil
}

// decodeShadowLoginStart 解析 B→影子 的 LoginStart，返回用户名。
func decodeShadowLoginStart(body []byte) (string, error) {
	id, payload, err := splitShadowPacket(body)
	if err != nil {
		return "", err
	}
	if id != idHSLoginStart {
		return "", fmt.Errorf("期望 LoginStart(id=0)，实得 id=%d", id)
	}
	r := codec.NewReader(payload)
	return r.String()
}

// splitShadowPacket 拆 (包 id, 载荷)。
func splitShadowPacket(body []byte) (int32, []byte, error) {
	r := codec.NewReader(body)
	id, err := r.VarInt()
	if err != nil {
		return 0, nil, err
	}
	return id, r.Rest(), nil
}

// offlineUUID 复刻 Java UUID.nameUUIDFromBytes("OfflinePlayer:"+name)
// （MD5 + version 3 + IETF variant）。MD5 在此是协议复刻，不是安全用途。
func offlineUUID(name string) []byte {
	sum := md5.Sum([]byte("OfflinePlayer:" + name))
	u := append([]byte(nil), sum[:]...)
	u[6] = (u[6] & 0x0f) | 0x30
	u[8] = (u[8] & 0x3f) | 0x80
	return u
}
