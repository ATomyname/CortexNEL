package ecpack

// GameIDs 是需要 nexus 介入资源包交付的服务器。
//
// 只收「服务端资源包里确实带客户端部署内容」的那台：
//   - 4688093584228529760 = EC小游戏·Java版1.21.8（/user-item-download-v2 返回
//     resource-pack.7z，实测 91MB、md5 80cf5ea7…，内含 config/bedrock-loader/ 与 mods/）
//
// 未纳入：4668698705152194374（EC小游戏·起床战争）—— 该服同接口返回的资源包字段为空，
// 没有可交付的包；纳入它只会让 relay 白改一个包里什么都没有的推送。
//
// 其它服务器一律不注册（见 Packets）：非 EC 服零改动。
var GameIDs = []string{
	"4688093584228529760",
}
