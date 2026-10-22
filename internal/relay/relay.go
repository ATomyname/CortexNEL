// Package relay 是 loopback 帧管线引擎（架构文档 §6）：
// 双 leg（client 明文腿 / server 加密腿）、编解码 stage、注册表、派发。
// 引擎本体协议中立（P1）：引擎文件内禁止出现服务器域名、brand、GameID 字面量；
// 服务器知识只出现在 packet/<server>/（P2）。
package relay
