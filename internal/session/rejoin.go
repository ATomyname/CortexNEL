// 0xff 会话过期重登分类（facts §4 + DR-1）：
// 0xff 判定会话死（可重登，受凭据策略约束）；0x11 盐错、0x10 join 内容错——不触发重登。
package session

// RejoinDecision 是一次 init/join 失败后的处置决策。
type RejoinDecision struct {
	AllowRelogin bool
	Reason       string
}

// ClassifyStatus 按状态字节分类（facts §4 状态码分类学）。
func ClassifyStatus(status byte) RejoinDecision {
	switch status {
	case 0x00:
		return RejoinDecision{AllowRelogin: false, Reason: "通过"}
	case 0x11:
		return RejoinDecision{AllowRelogin: false, Reason: "init 签名/盐被拒(0x11)，重登无意义"}
	case 0xff:
		// 是否真的重登由编排层按 DR-1（凭据落盘 opt-in）决定。
		return RejoinDecision{AllowRelogin: true, Reason: "登录会话过期(0xff)"}
	case 0x10:
		return RejoinDecision{AllowRelogin: false, Reason: "join 被拒(0x10)：modInfo/房间问题"}
	default:
		return RejoinDecision{AllowRelogin: false, Reason: "未知状态"}
	}
}
