// Package shadow 是影子端引擎的帧级部分。
//
// 职责边界（walkthrough/12 §12.4）：录制、重放、假登录、上行闸门。
// 本包**协议中立**（P1）：不出现任何服务器域名、brand 或 GameID 字面量；
// 需要「哪个包号是锚点」这类版本知识时一律由调用方注入。
//
// 与引擎的关系：本包依赖 relay 的两个只读接缝（Conn.OnClientBoundFrame /
// Conn.C2SGate），但 relay 引擎**不**依赖本包（依赖方向：shadow → relay）。
package shadow

// DefaultBudget 是默认录制预算（64MiB）：超出后按锚点裁剪，见 walkthrough/12 §12.5。
const DefaultBudget = 64 << 20
