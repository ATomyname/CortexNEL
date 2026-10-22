import { defineStore } from 'pinia'
import type { LogEntry } from '../lib/bus'

/**
 * 日志环形缓冲（架构文档 §10.5）：上限 5000 行，超出丢弃最旧并计数。
 * 批量刷新由 LogStream 组件负责，store 只管存量数据。
 */
const MAX_LINES = 5000

export const useLogStore = defineStore('log', {
  state: () => ({
    lines: [] as LogEntry[],
    /** 因超出上限被丢弃的行数，UI 据此提示。 */
    dropped: 0,
    paused: false,
  }),

  getters: {
    count: (s) => s.lines.length,
  },

  actions: {
    push(entry: LogEntry): void {
      if (this.paused) return
      // debug 默认不入缓冲；由 UI 侧过滤决定是否展示，此处只做容量控制。
      this.lines.push(entry)
      if (this.lines.length > MAX_LINES) {
        const overflow = this.lines.length - MAX_LINES
        this.lines.splice(0, overflow)
        this.dropped += overflow
      }
    },

    setPaused(v: boolean): void {
      this.paused = v
    },

    clear(): void {
      this.lines = []
      this.dropped = 0
    },
  },
})
