// Yggdrasil TCP 流程编排（facts §3）：authserver 列表、init/join 会话建立与状态码分类。
package netease

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// AuthServer 是 authserver 列表项（facts §3.1）。
type AuthServer struct {
	IP   string `json:"IP"`
	Port int    `json:"Port"`
}

// ErrSessionDead 是 init 0xff（会话过期，触发重登分类，facts §4）。
var ErrSessionDead = errors.New("netease: 登录会话过期(0xff)")

// ErrBadSignature 是 init 0x11（签名/盐错）。
var ErrBadSignature = errors.New("netease: init 签名被拒(0x11)")

// ErrJoinRejected 是 join 0x10（modInfo/房间错）。
var ErrJoinRejected = errors.New("netease: join 被拒(0x10)")

// FetchAuthServers 拉取 authserver 列表（四台等价，facts §3.1）。
func FetchAuthServers(ctx context.Context) ([]AuthServer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, AuthServerURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var servers []AuthServer
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&servers); err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		return nil, errors.New("netease: authserver 列表为空")
	}
	return servers, nil
}

// JoinParams 是一次 join 的全部输入。
type JoinParams struct {
	Credentials Credentials
	GameID      int64  // 服务器档案提供（引擎本体不保存 GameID 字面量，P2）
	ServerID    string // sha1(serverId‖secret‖pubkey) 40-hex（facts §7.2 步骤 3）
	ModInfo     string // {"mods":[...]}（facts §3.7，空列表也是合法 JSON）
	MCVersion   string // Minecraft 协议版本（如 "1.21.8", "1.20", "1.12.2"）
}

// Join 建立 Yggdrasil 会话（facts §3.2 全流程）。
func Join(ctx context.Context, p JoinParams) error {
	servers, err := FetchAuthServers(ctx)
	if err != nil {
		return err
	}
	var lastErr error
	for _, s := range servers {
		addr := net.JoinHostPort(s.IP, fmt.Sprintf("%d", s.Port))
		dialer := net.Dialer{Timeout: 10 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			lastErr = err
			continue
		}
		err = joinOne(ctx, conn, p)
		_ = conn.Close()
		if err == nil {
			return nil
		}
		// 会话类错误换服务器也无意义（四台等价，facts §4），直接返回。
		if errors.Is(err, ErrSessionDead) || errors.Is(err, ErrBadSignature) || errors.Is(err, ErrJoinRejected) {
			return err
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("netease: 无可用 authserver")
	}
	return lastErr
}

// joinOne 在单条 authserver 连接上完成 init + join。
func joinOne(ctx context.Context, conn net.Conn, p JoinParams) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	_ = conn.SetDeadline(deadline)

	// 1. s2c 272B：loginSeed(16) ‖ signContent(256)。
	body, err := readLEFrame(conn)
	if err != nil {
		return fmt.Errorf("netease: 读会话质询: %w", err)
	}
	if len(body) < 272 {
		return fmt.Errorf("netease: 质询长度 %d < 272", len(body))
	}
	loginSeed := body[:16]
	signContent := body[16:272]

	// 2. init 帧（未加密，facts §3.3/§3.5）。
	creds := p.Credentials
	authToken := creds.AuthToken()
	authID, err := creds.AuthID()
	if err != nil {
		return err
	}
	seed, err := DeriveSeed(authToken, loginSeed)
	if err != nil {
		return err
	}
	ver := p.MCVersion
	if ver == "" {
		ver = MCVersion
	}
	signData, err := buildInitSignData(ver, loginSeed, signContent, authID, seed)
	if err != nil {
		return err
	}
	if err := writeLEFrame(conn, buildInitBody(ver, authID, seed, signData)); err != nil {
		return fmt.Errorf("netease: 发送 init: %w", err)
	}

	// 3. init 应答：int16-LE len=1 + status。
	statusBody, err := readLEFrame(conn)
	if err != nil {
		return fmt.Errorf("netease: 读 init 应答: %w", err)
	}
	if len(statusBody) < 1 {
		return errors.New("netease: init 应答为空")
	}
	switch statusBody[0] {
	case 0x00:
	case 0x11:
		return ErrBadSignature
	case 0xff:
		return ErrSessionDead
	default:
		return fmt.Errorf("netease: init 失败 status=0x%02X", statusBody[0])
	}

	// 4. join（type-9 ChaCha 帧，facts §3.4）。
	c2s, s2c := DeriveChaChaKeys(authToken, loginSeed)
	timeSec := int32(time.Now().Unix())
	hash := BuildHashData(ver, creds.UserID, timeSec, loginSeed, p.ModInfo)
	joinMsg := packChaChaFrame(9, buildJoinPayload(p, timeSec, hash), c2s)
	if _, err := conn.Write(joinMsg); err != nil {
		return fmt.Errorf("netease: 发送 join: %w", err)
	}

	// 5. join 应答（s2c key 解密）。
	reply, err := readLEFrame(conn)
	if err != nil {
		return fmt.Errorf("netease: 读 join 应答: %w", err)
	}
	frameType, payload, err := unpackChaChaFrame(reply, s2c)
	if err != nil {
		return err
	}
	if frameType != 9 || len(payload) < 1 {
		return fmt.Errorf("netease: join 应答异常 type=%d", frameType)
	}
	if payload[0] != 0x00 {
		if payload[0] == 0x10 {
			return ErrJoinRejected
		}
		return fmt.Errorf("netease: join 失败 status=0x%02X", payload[0])
	}
	return nil
}

// buildInitSignData 完成 facts §3.5 全流程：
// 公钥解 signContent → clientKey(19B)+校验和(32B) → sign=sha256(BuildSign) →
// 私钥原始运算(clientKey ‖ sign)，SignPadMode=1。
func buildInitSignData(mcVersion string, loginSeed, signContent []byte, authID int32, seed []byte) ([]byte, error) {
	serverPub, err := ServerPublicKey()
	if err != nil {
		return nil, err
	}
	decrypted, err := RawPublicDecrypt(serverPub, signContent)
	if err != nil {
		return nil, fmt.Errorf("netease: 解 signContent: %w", err)
	}
	const clientKeyLen, checksumLen = 19, 32
	if len(decrypted) < clientKeyLen+checksumLen {
		return nil, fmt.Errorf("netease: signContent 载荷 %dB 不足", len(decrypted))
	}
	clientKey := decrypted[:clientKeyLen]
	checksum := decrypted[clientKeyLen : clientKeyLen+checksumLen]
	seedSum := sha256.Sum256(loginSeed)
	if !equalBytes(checksum, seedSum[:]) {
		return nil, errors.New("netease: loginSeed 校验和不匹配")
	}
	clientPriv, err := ClientKey()
	if err != nil {
		return nil, err
	}
	sign := sha256.Sum256(BuildSign(mcVersion, authID, seed))
	return RawPrivateOp(clientPriv, concat(clientKey, sign[:]))
}

// buildInitBody 构造 init 帧体（字段顺序 facts §3.3；盒子版本 shortStr 长度 BE）。
func buildInitBody(mcVersion string, authID int32, seed, signData []byte) []byte {
	ver := mcVersion
	if ver == "" {
		ver = MCVersion
	}
	out := make([]byte, 0, 4+16+2+len(LauncherVersion)+1+len(Channel)+16+2+len(signData)+1+len(ver)+len(McVersionSalt))
	out = binary.LittleEndian.AppendUint32(out, uint32(authID))
	out = append(out, seed...)
	out = appendShortStringBE(out, LauncherVersion)
	out = append(out, byte(len(Channel)))
	out = append(out, Channel...)
	out = append(out, TcpSalt...)
	out = binary.LittleEndian.AppendUint16(out, uint16(len(signData)))
	out = append(out, signData...)
	out = append(out, byte(len(ver)))
	out = append(out, ver...)
	out = append(out, McVersionSalt...)
	return out
}

// buildJoinPayload 构造 join 帧载荷（字段顺序 facts §3.4）。
func buildJoinPayload(p JoinParams, timeSec int32, hash []byte) []byte {
	ver := p.MCVersion
	if ver == "" {
		ver = MCVersion
	}
	out := make([]byte, 0, 8+1+len(p.ServerID)+1+len(LauncherVersion)+1+len(ver)+4+32+5+len(p.ModInfo)+5+2+1+len(Channel))
	out = binary.LittleEndian.AppendUint64(out, uint64(p.GameID))
	out = append(out, byte(len(p.ServerID)))
	out = append(out, p.ServerID...)
	out = append(out, byte(len(LauncherVersion)))
	out = append(out, LauncherVersion...)
	out = append(out, byte(len(ver)))
	out = append(out, ver...)
	out = binary.LittleEndian.AppendUint32(out, uint32(timeSec))
	out = append(out, hash...)
	out = appendShortStringLE(out, p.ModInfo)
	out = appendShortStringLE(out, PrcCheck)
	out = append(out, 0x00, 0x00) // int16 0（facts §3.4）
	out = append(out, byte(len(Channel)))
	out = append(out, Channel...)
	return out
}
