import { defineStore } from 'pinia'
import type { AttachWaitingEvent, LostEvent, RejoinEvent, StageEvent } from '../lib/bus'
import * as sessionApi from '../api/session'

/**
 * 会话域 store（架构文档 §10.4）：事件经 bus.ts 落地，绑定调用走 api/session。
 *
 * 多代理模型：sessions 是全部运行中代理的数组，键为 sessionID（sess-N）。
 * 阶段名以后端推送为准（迁移链见架构文档 §7.1），此处只记录、不做迁移判定。
 */

/** 时间轴留痕上限：长会话防内存无界增长，语义与 logStore 的环形缓冲一致。 */
const MAX_STAGE_RECORDS = 500

/** 与后端 service.SessionInfo 对应（wailsjs 生成的 app.SessionInfoDTO）。 */
export interface ProxySession {
  sessionId: string
  serverId: string
  accountId: string
  roleName?: string
  listenAddr: string
  stage: string
  launchMirror: boolean
  startedAt: string
}

export const useSessionStore = defineStore('session', {
  state: () => ({
    /** 全部运行中的代理会话（代理管理页数据源）。 */
    sessions: [] as ProxySession[],
    /** 最近一次启动返回的会话 ID（供"停止"等单会话操作默认取用）。 */
    lastStartedID: '',
    /** 阶段迁移留痕（全局时间轴，超出上限丢弃最旧）。 */
    stageHistory: [] as StageEvent[],
    /** 最近一次上游断开（含重连尝试计数）。 */
    lastUpstreamLost: null as LostEvent | null,
    /** 最近一次下游（镜像客户端）断开。 */
    lastClientLost: null as LostEvent | null,
    /** 最近一次 0xff 触发的重登结果。 */
    lastRejoin: null as RejoinEvent | null,
    /** 客户端长时间未连入的提示（session.attach.waiting）；阶段推进或会话结束时清除。 */
    attachWaiting: null as AttachWaitingEvent | null,
    /** 最近一次操作错误（用户可读文案，S1）。 */
    error: '',
  }),

  getters: {
    /** 是否有任何代理在跑。 */
    hasSession: (s) => s.sessions.length > 0,
    /** 白端会话（一键进服拉起的那一个，至多一个）。 */
    mirrorSession: (s) => s.sessions.find((x) => x.launchMirror) ?? null,
    /** 最近启动的会话（没有则第一个）。 */
    current: (s) => s.sessions.find((x) => x.sessionId === s.lastStartedID) ?? s.sessions[0] ?? null,
    isLive: (s) => s.sessions.some((x) => x.stage === 'LIVE'),
  },

  actions: {
    /** 用后端返回的全量列表替换本地状态（启动/停止/轮询后调用）。 */
    replaceAll(list: ProxySession[]): void {
      this.sessions = list
    },

    /** 单个会话的阶段推送（session.stage 事件）。 */
    onStage(e: StageEvent): void {
      const id = e.sessionID
      const target = this.sessions.find((x) => x.sessionId === id)
      if (target) {
        target.stage = e.to
      }
      // 阶段一推进，"长时间未连入"提示即失效。
      if (e.to !== 'ATTACH') {
        this.attachWaiting = null
      }
      this.stageHistory.push(e)
      if (this.stageHistory.length > MAX_STAGE_RECORDS) {
        this.stageHistory.shift()
      }
    },

    onStart(e: { sessionID: string; serverID: string; gameID: string }): void {
      this.lastStartedID = e.sessionID
      // 具体字段由随后的 refresh() 补全；此处先占位，避免列表闪现"空会话"。
      if (!this.sessions.some((x) => x.sessionId === e.sessionID)) {
        this.sessions.push({
          sessionId: e.sessionID,
          serverId: e.serverID,
          accountId: '',
          listenAddr: '',
          stage: 'ATTACH',
          launchMirror: false,
          startedAt: new Date().toISOString(),
        })
      }
    },

    onStop(e: { sessionID: string; reason: string; durationMs: number }): void {
      this.sessions = this.sessions.filter((x) => x.sessionId !== e.sessionID)
      if (this.lastStartedID === e.sessionID) {
        this.lastStartedID = ''
      }
      this.attachWaiting = null
    },

    onUpstreamLost(e: LostEvent): void {
      this.lastUpstreamLost = e
    },

    onClientLost(e: LostEvent): void {
      this.lastClientLost = e
    },

    onRejoin(e: RejoinEvent): void {
      this.lastRejoin = e
    },

    onAttachWaiting(e: AttachWaitingEvent): void {
      // 提示是瞬态的：阶段推进或会话结束后由 onStage/onStop 清场，
      // 这里只保留最近一次供弹层展示。
      this.attachWaiting = e
    },

    /** 从后端拉取全部会话并替换本地状态。 */
    async refresh(): Promise<void> {
      this.error = ''
      try {
        this.sessions = await sessionApi.listSessions()
      } catch (e) {
        this.error = String(e)
      }
    },

    /** 一键进服（主档案组合 + 自动拉白端）。 */
    async start(): Promise<string> {
      this.error = ''
      try {
        const r = await sessionApi.startSession()
        this.lastStartedID = r.sessionId
        await this.refresh()
        return r.sessionId
      } catch (e) {
        this.error = String(e)
        throw e
      }
    },

    /** 仅启动代理（不拉白端），可多开。 */
    async startProxyOnly(): Promise<string> {
      this.error = ''
      try {
        const r = await sessionApi.startProxyOnly()
        this.lastStartedID = r.sessionId
        await this.refresh()
        return r.sessionId
      } catch (e) {
        this.error = String(e)
        throw e
      }
    },

    /** 指定服务器与账号启动（服务器页"进入服务器"用）。 */
    async startWithTarget(serverId: string, accountId: string, launchMirror = true): Promise<string> {
      this.error = ''
      try {
        const r = await sessionApi.startSessionWithTarget(serverId, accountId, launchMirror)
        this.lastStartedID = r.sessionId
        await this.refresh()
        return r.sessionId
      } catch (e) {
        this.error = String(e)
        throw e
      }
    },

    /** 停止指定会话；不传则停最近启动的那个。 */
    async stop(sessionId?: string): Promise<void> {
      const id = sessionId ?? this.lastStartedID ?? this.current?.sessionId
      if (!id) {
        this.error = '当前没有进行中的会话'
        return
      }
      try {
        await sessionApi.stopSession(id)
        this.sessions = this.sessions.filter((x) => x.sessionId !== id)
        if (this.lastStartedID === id) this.lastStartedID = ''
      } catch (e) {
        this.error = String(e)
        throw e
      }
    },

    /** 停止全部会话。 */
    async stopAll(): Promise<void> {
      try {
        await sessionApi.stopAllSessions()
        this.sessions = []
        this.lastStartedID = ''
      } catch (e) {
        this.error = String(e)
        throw e
      }
    },
  },
})
