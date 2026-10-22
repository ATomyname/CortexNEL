// 会话域薄封装（架构文档 §10.2）。
import * as binding from '../../wailsjs/go/app/App'
import { app } from '../../wailsjs/go/models'

export type StartSessionResult = app.StartSessionResult
export type SessionInfo = app.SessionInfoDTO

export function startSession(): Promise<StartSessionResult> {
  return binding.StartSession()
}

export function startProxyOnly(): Promise<StartSessionResult> {
  return binding.StartProxyOnly()
}

export function startSessionWithTarget(serverId: string, accountId: string, launchMirror: boolean): Promise<StartSessionResult> {
  return binding.StartSessionWithTarget(serverId, accountId, launchMirror)
}

/** 停止指定会话（多代理下每次只停一个）。 */
export function stopSession(sessionId: string): Promise<void> {
  return binding.StopSession(sessionId)
}

/** 停止全部会话。 */
export function stopAllSessions(): Promise<void> {
  return binding.StopAllSessions()
}

/** 拉取全部运行中的代理会话。 */
export function listSessions(): Promise<SessionInfo[]> {
  return binding.ListSessions()
}
