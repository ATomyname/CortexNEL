// Wails 事件桥出方向：telemetry 事件 → 前端 EventsOn（入口侧见 frontend/src/lib/bus.ts）。
package app

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"nexus/internal/telemetry"
)

// emit 把一个遥测事件推给前端。
// 载荷 = 统一载荷的 fields + ts（EVENTS.md §0：{ts, subject, result, fields}，
// 前端载荷类型见 bus.ts 的 StageEvent / LostEvent / LogEntry）。
func emit(ctx context.Context, e telemetry.Event) {
	if ctx == nil {
		return
	}
	runtime.EventsEmit(ctx, e.Name, eventPayload(e))
}

// eventPayload 组装前端载荷。复制而非复用 Fields：事件对象同时被总线环形历史
// 持有，原地追加键会引入并发写（History 与订阅回调并发）。
// ts 优先保留生产者已带的同名键（如 telemetry.log 自带 ts），否则补总线时间戳。
func eventPayload(e telemetry.Event) map[string]any {
	out := make(map[string]any, len(e.Fields)+1)
	for k, v := range e.Fields {
		out[k] = v
	}
	if _, ok := out["ts"]; !ok {
		out["ts"] = e.TS
	}
	return out
}
