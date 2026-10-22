// Package netease 是网易接入协议实现（架构文档 §8）。
// 所有常量与帧格式只取自 docs/facts/NETEASE-PROTOCOL.md（规范 §10.11），
// 禁止从旧代码注释或记忆抄值；与白名单参考实现冲突时以 facts 为准。
package netease

// CrcSalt 是当前版本 CRC 盐（facts §1）。
const CrcSalt = "1B1B102964A93C61F7A4859E3536AA54"

// TokenKey 是 token 派生密钥（facts §1，16B）。
var TokenKey = []byte{
	0xAC, 0x24, 0x9C, 0x69, 0xC7, 0x2C, 0xB3, 0xB4,
	0x4E, 0xC0, 0xCC, 0x6C, 0x54, 0x3A, 0x81, 0x95,
}

// Skip32Key 是 authId 派生密钥（facts §1）。
var Skip32Key = []byte("SaintSteve")

// ChaChaNonce 是 Yggdrasil 帧加密 nonce（facts §1，12B）。
var ChaChaNonce = []byte("163 NetEase\n")

// McVersionSalt 是 1.20 实时客户端捕获盐（facts §1）。
// 红线：Nirvana 内嵌的旧盐 01 00 04 80 D2 3E F7 11 01 不可用（facts 明确标注）。
var McVersionSalt = []byte{0x01, 0x44, 0x50, 0xA5, 0x52, 0x00, 0x63, 0xB0, 0x40}

// TcpSalt 是 init 帧盐（facts §1，16B）。
var TcpSalt = []byte{
	0x2f, 0x84, 0xae, 0xa3, 0x99, 0x21, 0x29, 0x26,
	0xda, 0xbf, 0x95, 0xa3, 0xab, 0xaf, 0x37, 0xe0,
}

// 协议字符串常量（facts §1/§3）。
const (
	Channel         = "netease"                                        // join/init 的 channel 字段
	LauncherVersion = "1.15.28.11160"                                  // 盒子版本（facts §1）
	MCVersion       = "1.20"                                           // MC 版本（facts §1）
	MasterTypeGame  = "2"                                              // 网络服务器主类型（facts §1）
	PrcCheck        = "[]"                                             // join 帧固定字段（facts §3.4）
	AuthServerURL   = "https://x19.update.netease.com/authserver.list" // facts §3.1
)

// MD5 映射（facts §1 与 Nirvana Md5Mapping）。
const (
	BootstrapMd5 = "2A7A476411A1687A56DC6848829C1AE4"
	DatFileMd5   = "D285CBF97D9BA30D3C445DBF1C342634"
)

type Md5Pair struct {
	BootstrapMd5 string
	DatFileMd5   string
}

var VersionMd5Map = map[string]Md5Pair{
	"1.7.10": {"A895FE657915D58F55919CEACD30209D", "538D33D5F35EF01736EDA30F94C61DF6"},
	"1.8.9":  {"A895FE657915D58F55919CEACD30209D", "0CF2074AA7D4B543E35A3D6BB57AF861"},
	"1.8":    {"A895FE657915D58F55919CEACD30209D", "0CF2074AA7D4B543E35A3D6BB57AF861"},
	"1.12.2": {"A895FE657915D58F55919CEACD30209D", "51581ADD89B8AC5A0D8CCDD0E33EE1DE"},
	"1.16":   {"7B101583C3965371B89A3C9115B27526", "B0712F34B0A584D05D9D29FA68759E29"},
	"1.18":   {"C3BD2115F23F6FE4B2ADCC7FC4DEFFEA", "56677A2BB31E18246FA241FB02E16D0E"},
	"1.20":   {"2A7A476411A1687A56DC6848829C1AE4", "D285CBF97D9BA30D3C445DBF1C342634"},
	"1.20.1": {"2A7A476411A1687A56DC6848829C1AE4", "D285CBF97D9BA30D3C445DBF1C342634"},
	"1.20.6": {"2A7A476411A1687A56DC6848829C1AE4", "D285CBF97D9BA30D3C445DBF1C342634"},
	"1.21":   {"684528BF492A84489F825F5599B3E1C6", "574033E7E4841D8AC4C14D7FA5E05337"},
	"1.21.8": {"5BF6153C69DD28951A699F7F834EFE1A", "C9906B5809A92C73299279E562A78D81"},
}

func GetMd5Pair(version string) Md5Pair {
	for k, v := range VersionMd5Map {
		if k == version {
			return v
		}
	}
	for k, v := range VersionMd5Map {
		if len(k) > 3 && len(version) >= len(k) && (k == version || version[:len(k)] == k) {
			return v
		}
	}
	return Md5Pair{
		BootstrapMd5: BootstrapMd5,
		DatFileMd5:   DatFileMd5,
	}
}

// HttpAESKeys 是 x19 Core HTTP 加密传输的 16 把 AES-128 密钥（facts §6.1.2，
// 顺序敏感；来源 Nirvana HttpUtil.SKeys 逐键核对）。
var HttpAESKeys = [16][]byte{
	[]byte("MK6mipwmOUedplb6"), []byte("OtEylfId6dyhrfdn"),
	[]byte("VNbhn5mvUaQaeOo9"), []byte("bIEoQGQYjKd02U0J"),
	[]byte("fuaJrPwaH2cfXXLP"), []byte("LEkdyiroouKQ4XN1"),
	[]byte("jM1h27H4UROu427W"), []byte("DhReQada7gZybTDk"),
	[]byte("ZGXfpSTYUvcdKqdY"), []byte("AZwKf7MWZrJpGR5W"),
	[]byte("amuvbcHw38TcSyPU"), []byte("SI4QotspbjhyFdT0"),
	[]byte("VP4dhjKnDGlSJtbB"), []byte("UXDZx4KhZywQ2tcn"),
	[]byte("NIK73ZNvNqzva4kd"), []byte("WeiW7qU766Q1YQZI"),
}

// HttpIV 是 HTTP 加密传输的固定 IV（facts §6.1.2）。
var HttpIV = []byte("szkgpbyimxavqjcn")
