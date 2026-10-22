// 包模型与契约类型（架构文档 §5.1/§5.2）：三态基类 + GameID 作用域注册键。
package relay

import "nexus/internal/relay/codec"

// State 是 MC 连接状态（facts §7.2；763 无 Configuration 态，保留枚举供转译扩展）。
type State int

const (
	StateHandshake State = iota
	StateStatus
	StateLogin
	StatePlay
	StateConfiguration
)

// Direction 是包方向：以引擎视角，从客户端来为 ServerBound，从上游来为 ClientBound。
type Direction int

const (
	ServerBound Direction = iota
	ClientBound
)

// VersionAny 表示不限协议版本；锁定 763（DR-6）。
const VersionAny int32 = -1

// PacketKey 是包处理器匹配键。GameID 为 "" 通配所有服务器；
// PacketID 为 -1 通配该状态下所有包。全条件与匹配（架构文档 §5.2）。
type PacketKey struct {
	State    State
	Dir      Direction
	PacketID int32
	GameID   string
	Versions []int32
}

// Packet 是管线中的包处理器：命中后按 Read → Handle → Write 重新序列化。
// Handle 返回 cancel=true 表示该包不再向对端转发（丢弃/代答由处理器自行完成）；
// 返回 error 视为管线故障，连接关闭。
type Packet interface {
	Read(c *Conn, r *codec.Reader) error
	Handle(c *Conn) (cancel bool, err error)
	Write(w *codec.Writer) error
}

// RawForward 内嵌结构：缓存原始字节并原样回吐（架构文档 §5.1），
// 用于字段众多、只读关心个别字段的大包。
type RawForward struct {
	raw []byte
}

// ReadAll 消费全部剩余字节作为原始缓存。
func (f *RawForward) ReadAll(r *codec.Reader) error {
	f.raw = append([]byte(nil), r.Rest()...)
	return nil
}

// WriteRaw 把原始缓存写回。
func (f *RawForward) WriteRaw(w *codec.Writer) {
	if len(f.raw) > 0 {
		w.Fixed(f.raw)
	}
}

// NoForward 内嵌结构：读写皆空实现（架构文档 §5.1），
// 用于需要吞掉并自行应答的包（如 EncryptionRequest）。
type NoForward struct{}

// Read 不消费任何字段。
func (NoForward) Read(*Conn, *codec.Reader) error { return nil }

// Handle 默认不取消；吞包处理器自行覆写并返回 cancel=true。
func (NoForward) Handle(*Conn) (bool, error) { return false, nil }

// Write 不产生任何字节。
func (NoForward) Write(*codec.Writer) error { return nil }

// PacketWriter 是仅编码接口：注入型包（如代答 EncryptionResponse）不需要 Read/Handle。
type PacketWriter interface {
	Write(w *codec.Writer) error
}

// Registered 是注册表条目：工厂每次派发实例化，避免跨包状态串扰（P3 确定性）。
type Registered struct {
	Key     PacketKey
	Factory func() Packet
}
