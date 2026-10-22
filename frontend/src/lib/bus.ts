/**
 * 事件桥：Wails 推送通道（EventsOn）到 Pinia store 的唯一入口。
 *
 * 约束（架构文档 §10.4）：组件只读 store，禁止直接订阅事件。
 * 这样事件处理逻辑集中在 store action 中，可独立于 UI 测试。
 *
 * 事件名以后端事件名（internal/telemetry）为单源，前后端改动同步。
 */
import { EventsOn } from '../../wailsjs/runtime'
import { useInstallStore, type InstallProgress } from '../stores/installStore'
import { useLogStore } from '../stores/logStore'
import { useSessionStore } from '../stores/sessionStore'

/** 遥测日志行，字段与 internal/telemetry 的 telemetry.log 事件一致。 */
export interface LogEntry {
  ts: string
  level: 'debug' | 'info' | 'warn' | 'error'
  module: string
  message: string
}

/** 会话阶段迁移事件载荷（EVENTS.md §1；sessionID 即会话 ID，sess-N）。 */
export interface StageEvent {
  ts: string
  from: string
  to: string
  sessionID: string
}

/** 连接断开事件载荷。 */
export interface LostEvent {
  ts: string
  sessionID: string
  err?: string
  attempt?: number
}

export interface RejoinEvent {
  ts: string
  sessionID: string
  reason: string
  result: string
}

/** 客户端长时间未连入的提示事件载荷（EVENTS.md §1 session.attach.waiting）。 */
export interface AttachWaitingEvent {
  ts: string
  sessionID: string
  waitedMs: number
  launchMirror: boolean
}

const logStore = () => useLogStore()
const sessionStore = () => useSessionStore()
const installStore = () => useInstallStore()

/**
 * 注册所有事件订阅，返回取消订阅函数。
 * 在应用挂载前调用，避免漏掉早期推送。
 */
export function registerEventBridge(): () => void {
  // 浏览器开发预览没有 Wails runtime；保持页面可浏览，绑定调用仍由桌面容器提供。
  if (!(window as Window & { go?: unknown }).go) return () => undefined
  const offs: Array<() => void> = []

  offs.push(EventsOn('telemetry.log', (e: LogEntry) => logStore().push(e)))

  offs.push(EventsOn('session.stage', (e: StageEvent) => sessionStore().onStage(e)))
  offs.push(EventsOn('session.start', (e: { sessionID: string; serverID: string; gameID: string }) =>
    sessionStore().onStart(e),
  ))
  offs.push(EventsOn('session.stop', (e: { sessionID: string; reason: string; durationMs: number }) =>
    sessionStore().onStop(e),
  ))
  offs.push(EventsOn('session.upstream.lost', (e: LostEvent) => sessionStore().onUpstreamLost(e)))
  offs.push(EventsOn('session.client.lost', (e: LostEvent) => sessionStore().onClientLost(e)))
  offs.push(EventsOn('session.rejoin', (e: RejoinEvent) => sessionStore().onRejoin(e)))
  offs.push(EventsOn('session.attach.waiting', (e: AttachWaitingEvent) => sessionStore().onAttachWaiting(e)))
  // 客户端本体与服务器组件的下载/校验/解压进度。
  offs.push(EventsOn('install.progress', (e: InstallProgress & { ts: string }) => installStore().onProgress(e)))

  return () => offs.forEach((off) => off())
}
