// L2 回环验收（架构文档 §14）：内置 fake 上游 + 真实引擎管线，
// 单条客户端连接走通 handshake → 登录（加密代答）→ 压缩 → Play 双向转发 + 适配包行为。
package heypixel_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
	"nexus/internal/relay/packet/heypixel"
	"nexus/internal/relay/packet/login"
	"nexus/internal/relay/stage"
	"nexus/internal/telemetry"
)

const (
	loopThreshold  = 256
	loopServerAddr = "mirror.placeholder.invalid" // fake 上游握手地址；服务器域名常量只允许出现在 packet/heypixel（P1 门禁：引擎目录 grep 零命中）
)

type fakeJoiner struct {
	gotRequest bool
	secret     []byte
}

func (j *fakeJoiner) OnEncryptionRequest(_ *relay.Conn, _ relay.EncryptionRequestData) ([]byte, error) {
	j.gotRequest = true
	return j.secret, nil
}

func TestPipelineLoopbackLoginToPlay(t *testing.T) {
	secret := bytes.Repeat([]byte{0x42}, 16)
	joiner := &fakeJoiner{secret: secret}
	live := make(chan struct{})
	upstreamGotBrand := make(chan []byte, 1)

	upListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upListener.Close()
	upstreamErr := make(chan error, 1)
	go func() { upstreamErr <- runFakeUpstream(t, upListener, secret, upstreamGotBrand) }()

	bus := telemetry.NewBus()
	logDir := t.TempDir()
	logger, err := telemetry.New(bus, logDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = logger.Close()
		if t.Failed() {
			entries, _ := filepath.Glob(filepath.Join(logDir, "logs", "*.jsonl"))
			for _, f := range entries {
				data, _ := os.ReadFile(f)
				t.Logf("引擎日志:\n%s", data)
			}
		}
	})
	rg := &relay.Registry{}
	rg.Register(login.Packets()...)
	rg.Register(heypixel.Packets()...)

	interceptor := relay.NewInterceptor("127.0.0.1:0", rg, bus, logger, func(net.Conn) (*relay.Acceptance, error) {
		return &relay.Acceptance{
			ID:           "loop-conn",
			UpstreamAddr: upListener.Addr().String(),
			GameID:       heypixel.GameID,
			Joiner:       joiner,
			OnLoginSuccess: func(*relay.Conn) {
				close(live)
			},
		}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = interceptor.ListenAndServe(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for interceptor.Addr() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if interceptor.Addr() == nil {
		t.Fatal("拦截器未就绪")
	}

	runFakeClient(t, interceptor.Addr().String(), upstreamGotBrand)

	select {
	case <-live:
	default:
		t.Fatal("OnLoginSuccess 未触发")
	}
	if !joiner.gotRequest {
		t.Fatal("Joiner 未被调用")
	}
	select {
	case err := <-upstreamErr:
		if err != nil {
			t.Fatalf("fake 上游错误: %v", err)
		}
	default:
	}
	interceptor.Stop()
}

// --- fake 上游：握手 → EncReq → 验 EncResp → 切加密 → SetCompression →
// LoginSuccess → 收 brand（断言改写）→ 回未注册包 → 发 collideRule 队伍包 ---

func runFakeUpstream(t *testing.T, ln net.Listener, secret []byte, gotBrand chan<- []byte) error {
	t.Helper()
	conn, err := ln.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()

	// handshake + loginStart（明文）。
	if _, err := readLegFrame(conn, nil, nil); err != nil {
		return fmt.Errorf("读 handshake: %w", err)
	}
	if _, err := readLegFrame(conn, nil, nil); err != nil {
		return fmt.Errorf("读 loginStart: %w", err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return err
	}
	encReq := appendPacket(nil, 1, func() []byte {
		out := mcString("") // serverId 为空（布吉岛语义）
		out = append(out, varintBytes(int32(len(pubDER)))...)
		out = append(out, pubDER...)
		out = append(out, 0x00) // verifyToken 空
		return out
	}())
	if err := writeLegFrame(conn, nil, nil, encReq); err != nil {
		return err
	}

	respBody, err := readLegFrame(conn, nil, nil)
	if err != nil {
		return fmt.Errorf("读 EncResp: %w", err)
	}
	// payload = varint(id=1) ‖ varint(secretLen) ‖ secret ‖ varint(tokenLen) ‖ token
	respR := codec.NewReader(respBody)
	if _, err := respR.VarInt(); err != nil {
		return fmt.Errorf("EncResp 包 ID: %w", err)
	}
	encSecret, err := respR.ByteArray()
	if err != nil {
		return fmt.Errorf("EncResp secret 段: %w", err)
	}
	decSecret, err := rsa.DecryptPKCS1v15(rand.Reader, key, encSecret)
	if err != nil {
		return fmt.Errorf("解密 secret: %w", err)
	}
	if !bytes.Equal(decSecret, secret) {
		t.Fatalf("上游解出的 secret 与 Joiner 返回不一致: % x", decSecret)
	}

	pair, err := stage.NewCipherPair(secret)
	if err != nil {
		return err
	}
	comp := &stage.Compressor{Threshold: loopThreshold}

	// SetCompression：加密、未压缩帧格式（发送方在告知阈值前不用压缩帧）。
	if err := writeLegFrame(conn, pair, nil, appendPacket(nil, 3, varintBytes(loopThreshold))); err != nil {
		return err
	}
	// LoginSuccess：加密 + 压缩帧格式。
	success := appendPacket(nil, 2, append(bytes.Repeat([]byte{0xAB}, 16), mcString("TestPlayer")...))
	if err := writeLegFrame(conn, pair, comp, success); err != nil {
		return err
	}

	// 收 brand（应已被改写为 forge）。
	got, err := readLegFrame(conn, pair, comp)
	if err != nil {
		return fmt.Errorf("读 brand: %w", err)
	}
	if !bytes.Contains(got, []byte("forge")) || bytes.Contains(got, []byte("vanilla")) {
		t.Fatalf("brand 未被正确改写: % x", got)
	}
	gotBrand <- got

	// 回一个未注册 S2C 包（0x26，验证未知包透传）→ 发 collideRule 队伍包（应被引擎吞掉）。
	if err := writeLegFrame(conn, pair, comp, appendPacket(nil, 0x26, mcString("pong"))); err != nil {
		return err
	}
	if err := writeLegFrame(conn, pair, comp, appendPacket(nil, 92, mcString("collideRule_x"))); err != nil {
		return err
	}
	time.Sleep(300 * time.Millisecond)
	return nil
}

// --- fake 镜像客户端：单条连接走完全流程 ---

func runFakeClient(t *testing.T, addr string, gotBrand <-chan []byte) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	hs := appendPacket(nil, 0, func() []byte {
		out := varintBytes(763)
		out = append(out, mcString(loopServerAddr)...)
		var port [2]byte
		binary.BigEndian.PutUint16(port[:], 25565)
		out = append(out, port[:]...)
		return append(out, varintBytes(2)...)
	}())
	if err := writeLegFrame(conn, nil, nil, hs); err != nil {
		t.Fatal(err)
	}
	if err := writeLegFrame(conn, nil, nil, appendPacket(nil, 0, mcString("TestPlayer"))); err != nil {
		t.Fatal(err)
	}

	// SetCompression 以未压缩帧格式到达（引擎时序正确性断言）。
	sc, err := readLegFrame(conn, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sc) < 2 || sc[0] != 3 {
		t.Fatalf("SetCompression 应以未压缩帧格式先到: % x", sc)
	}

	comp := &stage.Compressor{Threshold: loopThreshold}
	// LoginSuccess（压缩帧格式）。
	ls, err := readLegFrame(conn, nil, comp)
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) < 1 || ls[0] != 2 {
		t.Fatalf("第二个包应是 LoginSuccess: % x", ls)
	}

	// 客户端发 brand（vanilla，763 play C2S custom_payload = 0x0D = 13，facts §7.6），引擎应改写为 forge 后转上游。
	brand := appendPacket(nil, 13, func() []byte {
		out := mcString("minecraft:brand")
		return append(out, 0x07, 'v', 'a', 'n', 'i', 'l', 'l', 'a')
	}())
	if err := writeLegFrame(conn, nil, comp, brand); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gotBrand:
	case <-time.After(5 * time.Second):
		t.Fatal("上游未收到 brand 包")
	}

	// 收未注册包（0x26 验证未知包透传）。
	unknown, err := readLegFrame(conn, nil, comp)
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown) < 1 || unknown[0] != 0x26 {
		t.Fatalf("应收到未注册包(0x26): % x", unknown)
	}

	// collideRule 队伍包被吞：1.5s 内不应再有包。
	_ = conn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	if extra, err := readLegFrame(conn, nil, comp); err == nil {
		t.Fatalf("collideRule 队伍包未被吞掉: % x", extra)
	}
}

// --- 帧 IO 助手（加密腿长度前缀逐字节流经密钥流，facts §7.3） ---

func readLegFrame(r io.Reader, c *stage.CipherPair, comp *stage.Compressor) ([]byte, error) {
	var length, shift int
	head := make([]byte, 1)
	for i := 0; i < 5; i++ {
		if _, err := io.ReadFull(r, head); err != nil {
			return nil, err
		}
		b := head[0]
		if c != nil {
			dec := []byte{b}
			c.TransformRead(dec)
			b = dec[0]
		}
		length |= int(b&0x7F) << shift
		shift += 7
		if b&0x80 == 0 {
			break
		}
	}
	if length <= 0 || length > 2<<20 {
		return nil, io.ErrUnexpectedEOF
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	if c != nil {
		c.TransformRead(body)
	}
	if comp != nil {
		return comp.Decode(body)
	}
	return body, nil
}

func writeLegFrame(w io.Writer, c *stage.CipherPair, comp *stage.Compressor, withID []byte) error {
	body := withID
	var err error
	if comp != nil {
		if body, err = comp.Encode(withID); err != nil {
			return err
		}
	}
	frame := stage.WriteFrame(body)
	if c != nil {
		c.TransformWrite(frame)
	}
	_, err = w.Write(frame)
	return err
}

func varintBytes(v int32) []byte {
	var out []byte
	u := uint32(v)
	for {
		if u&^0x7F == 0 {
			return append(out, byte(u))
		}
		out = append(out, byte(u&0x7F)|0x80)
		u >>= 7
	}
}

func appendPacket(prefix []byte, id int32, payload []byte) []byte {
	out := append([]byte(nil), prefix...)
	out = append(out, varintBytes(id)...)
	return append(out, payload...)
}

func mcString(s string) []byte {
	out := varintBytes(int32(len(s)))
	return append(out, s...)
}
