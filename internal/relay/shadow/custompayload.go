// custompayload.go — 职责: B 腿的 CustomPayload 屏蔽（双向）。
//
// 为什么屏蔽：影子端 B 是中途接入的旁观者，它的 mod 握手对不上真实上游 ——
// B 若发 FML/模组注册，上游收到的是一个「没走过登录期 mod 协商的客户端」在补发注册，
// 轻则被忽略、重则被判定协议违规踢线；B 若收到上游发给 A 的握手回包，
// 它会按自己的 mod 集合去解释一份不属于它的数据。两头都不该通。
//
// 关键：屏蔽只作用于 **B 腿**，A 的录制内容必须原样保留
// （A 那边是真需要 mod 握手的）。因此这是净出/净入方向的**分流过滤**，
// 不是录制时过滤 —— 过滤器挂错了位置会静默把 A 也废掉。
//
// 版本知识不硬编码（P1）：三个包号由调用方注入（见 heypixel.CustomPayloadPacketIDs）。
package shadow

// CustomPayloadIDs 是 CustomPayload 在客户端各状态下所用的包号。
//
// 同一个语义在不同协议版本/状态下落在不同 ID 上（763 play 是 0x0D/0x18，
// 766+ 多了 configuration 态的 0x02），因此是集合而非单值。
// 未注入（全为负）时屏蔽退化为「不屏蔽」，这是刻意的：宁可不屏蔽，
// 也不能因为版本知识缺失而误屏蔽掉正常游戏包。
type CustomPayloadIDs struct {
	// ServerBound 是 B→上游方向的 CustomPayload 包号集合。
	ServerBound []int32
	// ClientBound 是上游→B 方向的 CustomPayload 包号集合。
	ClientBound []int32
}

// CustomPayloadFilter 按方向屏蔽 CustomPayload。
//
// 用法：一次构造，两个方向的判定方法分别挂到
// relay.Conn.C2SGate（B 腿上行）与 Viewer.SetFramePolicy（B 腿下行）上。
type CustomPayloadFilter struct {
	sb map[int32]struct{}
	cb map[int32]struct{}

	// 计数（诊断与测试断言：屏蔽静默失效是最危险的失败模式）。
	blockedSB int
	blockedCB int
}

// NewCustomPayloadFilter 构造过滤器。两个集合都为空时，所有判定都返回「放行」。
func NewCustomPayloadFilter(ids CustomPayloadIDs) *CustomPayloadFilter {
	f := &CustomPayloadFilter{
		sb: make(map[int32]struct{}, len(ids.ServerBound)),
		cb: make(map[int32]struct{}, len(ids.ClientBound)),
	}
	for _, id := range ids.ServerBound {
		// 负数包号（未知版本的哨兵值）不得进集合：否则 -1 会命中所有哨兵。
		if id >= 0 {
			f.sb[id] = struct{}{}
		}
	}
	for _, id := range ids.ClientBound {
		if id >= 0 {
			f.cb[id] = struct{}{}
		}
	}
	return f
}

// AllowServerBound 判定 B→上游的一帧是否放行。
// 直接赋给 relay.Conn.C2SGate 之前必须包一层「所有权 + 本过滤」（见 gate.go 的 AllowB）。
func (f *CustomPayloadFilter) AllowServerBound(id int32) bool {
	if f == nil || len(f.sb) == 0 {
		return true
	}
	_, hit := f.sb[id]
	return !hit
}

// AllowClientBound 判定上游→B 的一帧是否放行。
// 赋给 Viewer.SetFramePolicy 时，放行的帧需要由策略自己写回该腿。
func (f *CustomPayloadFilter) AllowClientBound(id int32) bool {
	if f == nil || len(f.cb) == 0 {
		return true
	}
	_, hit := f.cb[id]
	return !hit
}

// Stats 返回 (屏蔽的 B→上游 帧数, 屏蔽的 上游→B 帧数)。
func (f *CustomPayloadFilter) Stats() (blockedSB, blockedCB int) {
	if f == nil {
		return 0, 0
	}
	return f.blockedSB, f.blockedCB
}

// noteServerBoundBlocked 在放行判定之外单独记账：AllowXxx 是热路径上的纯判定，
// 由调用方（分流侧）在真正丢弃时调用，避免判定与丢弃口径不一致。
func (f *CustomPayloadFilter) noteServerBoundBlocked() {
	if f != nil {
		f.blockedSB++
	}
}

func (f *CustomPayloadFilter) noteClientBoundBlocked() {
	if f != nil {
		f.blockedCB++
	}
}
