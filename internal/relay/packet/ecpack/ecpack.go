// EC 资源包下发：把服务器推送的资源包改写为指向 nexus 本地分发的地址。
//
// 交付内容 = 服务端进服时下发的资源包**原样**（2026-10-03 起不再转换）：服务端的
// ViaBedrock 包本身就是原版合规包（pack_format 84 = 1.21.8，assets/viabedrock 等
// 加 bedrock/*.mcpack 原件），白端直接进服加载的就是它。nexus 只改写推送地址，
// 让没有 EC mod 的原版镜像客户端按原版机制自行下载应用 —— 不需要装任何 EC mod。
//
// 只在 EC 服务器生效：Packets 按调用方传入的 GameID 集合注册，其它服不注册、不改写。
package ecpack

import (
	"crypto/rand"
	"fmt"
	"strings"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

// 包 id：按原版 1.21.8（协议 772）源码逐条数出。
//
// ⚠️ 数法有陷阱：play 态 clientbound 的**第一个包 CLIENTBOUND_BUNDLE 用 withBundlePacket 注册**
// 而不是 addPacket，它占 id 0 —— 只数 addPacket 会整体少 1。实测代价：把 73 当 push 发出去，
// 客户端按 pop 解析并在多出 76 字节时报 "Network Protocol Error" 断连。
//
//	play 态 clientbound（含 bundle=0）：JoinGame=43，ResourcePackPop=73，ResourcePackPush=74
//	configuration 态 clientbound（无此类偏移，custom_payload=1 与公开协议值吻合）：Pop=8，Push=9
const (
	idResourcePackPushPlay   int32 = 74
	idResourcePackPushConfig int32 = 9
	// idJoinGamePlay 是 play 态 clientbound 的 JoinGame（ClientboundLoginPacket）id。
	// 它是客户端进 play 态后的第一个包，也是最早能处理资源包的时点 —— 主动注入就挂这里。
	idJoinGamePlay int32 = 43
	// protocol1218 是已实证的协议版本：**772 = 1.21.8**（实机握手日志 version=772 证实）。
	// 注意 768 不是 1.21.8（那是 1.21.2/1.21.3）—— 早先按 768 注册导致处理器一条都没命中。
	// 其它版本的同名包 id 未必相同，未实证前不注册，避免误改写无关包。
	protocol1218 int32 = 772
)

// maxHashLength 是原版对资源包 hash 字段的长度上限（MAX_HASH_LENGTH）。
const maxHashLength = 40

// Provider 提供要下发给客户端的资源包地址与内容 hash。
// 由上层（app 层）注入；ok=false 表示本次不改写（例如本地还没有可用的包，
// 或门控判定不该介入），此时包原样透传。
type Provider interface {
	PackURL() (url string, hash string, ok bool)
}

// ServerPackURLSink 可选接口：改写处理器发现服务端推送的资源包 URL 时通知实现方。
// 用于「下载服务端包 → 转换 → 由 nexus 重新分发」的链路（比 JoinGame 注入更可靠，
// 因为服务端一定会推自己的包）。
type ServerPackURLSink interface {
	OnServerPackURL(url string)
}

// Packets 返回资源包改写处理器，按传入的 GameID 作用域注册。
// gameIDs 为空时不注册任何处理器（等价于该功能关闭）。
func Packets(gameIDs []string, provider Provider) []relay.Registered {
	var out []relay.Registered
	for _, id := range gameIDs {
		if id == "" {
			continue
		}
		out = append(out,
			relay.Registered{
				Key: relay.PacketKey{
					State: relay.StatePlay, Dir: relay.ClientBound,
					PacketID: idResourcePackPushPlay, GameID: id,
					Versions: []int32{protocol1218},
				},
				Factory: func() relay.Packet { return &resourcePackPush{provider: provider} },
			},
			// ⚠️ play 态 id 9 在 772 实为 boss_event（原版表），此处按历史探针
			// 观察保留注册：探针曾在 play 视角见到 id 9 载荷含 http 资源包 URL
			// —— 但该观察发生于 0x69 字幕包误触发状态迁移的时期，"play 9"
			// 很可能是 configuration 推送（id 9 ✓ 原版正确）被错标状态。
			// 保留注册的前提是 Handle 的 sha1 守卫（见下）：真推送 hash 为
			// 40 位 sha1，boss_event 载荷不可能通过，命中即透传。
			relay.Registered{
				Key: relay.PacketKey{
					State: relay.StatePlay, Dir: relay.ClientBound,
					PacketID: 9, GameID: id,
					Versions: []int32{protocol1218},
				},
				Factory: func() relay.Packet { return &resourcePackPush{provider: provider} },
			},
			relay.Registered{
				Key: relay.PacketKey{
					State: relay.StateConfiguration, Dir: relay.ClientBound,
					PacketID: idResourcePackPushConfig, GameID: id,
					Versions: []int32{protocol1218},
				},
				Factory: func() relay.Packet { return &resourcePackPush{provider: provider} },
			},
			relay.Registered{
				Key: relay.PacketKey{
					State: relay.StatePlay, Dir: relay.ClientBound,
					PacketID: idJoinGamePlay, GameID: id,
					Versions: []int32{protocol1218},
				},
				Factory: func() relay.Packet { return &joinGameInjector{provider: provider} },
			},
		)
	}
	return out
}

// joinGameInjector 在 JoinGame 转发给客户端之后，主动注入一条资源包推送。
//
// 为什么需要「主动注入」：EC 的资源包走的是平台侧下发（启动器部署 .minecraft），
// 游戏连接里未必存在服务端资源包推送 —— 只改写「已存在的推送」会什么都不发生。
// JoinGame 是客户端进 play 态后的第一个包，是能确定生效的最早时点。
//
// 代价与取舍：若服务端**也**推了自己的资源包，客户端会先后拿到两份。这里选择照推，
// 因为本包是唯一带「已转成原版几何/贴图」内容的那份；服务端那份对原版客户端本就无用。
type joinGameInjector struct {
	provider Provider
	raw      []byte
}

func (p *joinGameInjector) Read(_ *relay.Conn, r *codec.Reader) error {
	p.raw = append([]byte(nil), r.Rest()...)
	return nil
}

func (p *joinGameInjector) Handle(c *relay.Conn) (bool, error) {
	if p.provider == nil {
		return false, nil
	}
	url, hash, ok := p.provider.PackURL()
	if !ok {
		return false, nil
	}
	payload, err := buildPush(url, hash)
	if err != nil {
		return false, err
	}
	c.QueueClientInjection(idResourcePackPushPlay, payload)
	c.Logger().Info("relay", "已排队向客户端注入资源包推送",
		"connID", c.ID, "url", url, "sha1", hash)
	return false, nil
}

func (p *joinGameInjector) Write(w *codec.Writer) error {
	w.Fixed(p.raw)
	return nil
}

// buildPush 构造 ClientboundResourcePackPushPacket 载荷：
// UUID | String url | String hash | bool required | Optional<Component> prompt(缺省)。
//
// required=false 是刻意的：required=true 时玩家拒绝会被踢出。原版语义
// （ClientCommonPacketListenerImpl.handleResourcePackPush）是——该服状态非 PROMPT
// 且（不强制或未禁用）即自动下载应用，否则弹一次确认。
func buildPush(url, hash string) ([]byte, error) {
	if url == "" {
		return nil, fmt.Errorf("资源包 URL 为空")
	}
	if len(hash) > maxHashLength {
		return nil, fmt.Errorf("资源包 hash 超长（%d > %d）", len(hash), maxHashLength)
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	w := codec.NewWriter()
	w.Fixed(id[:])
	w.String(url)
	w.String(hash)
	w.Bool(false)
	w.Byte(0x00) // Optional<Component>：prompt 缺省
	return w.Bytes(), nil
}

// isSha1Hex 判断 s 是否为 40 位十六进制（sha1 指纹）。资源包推送的 hash 字段
// 恒为 sha1，用它区分真推送与撞上同 id 的其他包（772 的 play 9 = boss_event）。
func isSha1Hex(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// resourcePackPush 是 ClientboundResourcePackPushPacket 的改写器。
//
// 线格式（原版 1.21.8 ClientboundResourcePackPushPacket.STREAM_CODEC）：
//
//	UUID id | String url | String hash(≤40) | bool required | Optional<Component> prompt
//
// prompt 是可选组件，编码长度随内容变化；本处理器不改它，按原始字节原样回写
// （relay.RawForward 的同一思路：字段众多、只关心个别字段）。
type resourcePackPush struct {
	provider Provider

	raw      []byte // 原始载荷（容错：解析失败时按原样回写）
	valid    bool
	id       []byte
	url      string
	hash     string
	required bool
	tail     []byte
}

func (p *resourcePackPush) Read(_ *relay.Conn, r *codec.Reader) error {
	p.raw = append([]byte(nil), r.Rest()...)
	raw, err := r.Fixed(16)
	if err != nil {
		return nil // 容错：不是推送的包，原样透传
	}
	p.id = append([]byte(nil), raw...)
	if p.url, err = r.String(); err != nil {
		return nil
	}
	if p.hash, err = r.String(); err != nil {
		return nil
	}
	if p.required, err = r.Bool(); err != nil {
		return nil
	}
	p.tail = append([]byte(nil), r.Rest()...)
	p.valid = true
	return nil
}

func (p *resourcePackPush) Handle(c *relay.Conn) (bool, error) {
	if p.provider == nil || !p.valid {
		return false, nil
	}
	// 只处理真的资源包推送。play 态 id 9 在 772 实为 boss_event（原版表，
	// Prisma­rineJS 与 GameProtocols 链序双重验证）—— 载荷若被误判改写，
	// 客户端按 boss_event 解析畸形帧会直接崩（2026-10-05 实测
	// "fieldSize is too long! Length is 28530, but maximum is 292"）。
	// 真推送的 hash 是 40 位 sha1（EC 两台后端实测样本均如此），据此二次把关；
	// 同时记录命中的包 id，供持续核对 EC 到底在哪个 id 上推送。
	if !strings.Contains(p.url, "http://") && !strings.Contains(p.url, "https://") {
		return false, nil
	}
	if !isSha1Hex(p.hash) {
		c.Logger().Info("relay", "疑似非资源包推送（hash 非 sha1），原样透传",
			"connID", c.ID, "packetID", c.CurrentPacketID(), "url", p.url)
		return false, nil
	}
	// 服务端推的 URL 先通知 sink（app 侧据此下载并转换其资源包）。
	if sink, ok := p.provider.(ServerPackURLSink); ok && p.url != "" {
		sink.OnServerPackURL(p.url)
	}
	c.Logger().Info("relay", "服务端资源包推送", "connID", c.ID, "packetID", c.CurrentPacketID(), "url", p.url, "hash", p.hash, "required", p.required)
	url, hash, ok := p.provider.PackURL()
	if !ok {
		return false, nil
	}
	c.Logger().Info("relay", "服务端资源包改写为 nexus 本地分发",
		"connID", c.ID, "server", p.url, "local", url)
	p.url, p.hash = url, hash
	return false, nil
}

func (p *resourcePackPush) Write(w *codec.Writer) error {
	if !p.valid {
		w.Fixed(p.raw)
		return nil
	}
	w.Fixed(p.id)
	w.String(p.url)
	w.String(p.hash)
	w.Bool(p.required)
	w.Fixed(p.tail)
	return nil
}
