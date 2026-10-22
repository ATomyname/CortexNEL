import { defineStore } from 'pinia'

/**
 * 进服前置资产的补齐进度（事件 install.progress，EVENTS.md §7）。
 *
 * 本地缺客户端本体或缺该服组件时，后端会下载补齐；这个 store 只负责把进度
 * 呈现给用户——补齐一旦失败，进服会直接返回可读错误，不会静默退回别的客户端。
 */
export interface InstallProgress {
  /** download / verify / extract / done */
  stage?: string
  /** client:<版本> / component:<iid> */
  subject?: string
  done?: number
  total?: number
  message?: string
}

export const useInstallStore = defineStore('install', {
  state: () => ({
    stage: '' as string,
    subject: '' as string,
    done: 0,
    total: 0,
    message: '',
    /** 最近一次进度的时间戳，用于判断是否仍在进行。 */
    updatedAt: '',
  }),

  getters: {
    /** 有活干：阶段非空且不是 done。 */
    active: (s) => s.stage !== '' && s.stage !== 'done',
    /**
     * 完成百分比；total 未知（<=0）时返回 null ——
     * 此时 UI 必须展示不确定进度，不得编造百分比（规范 U4）。
     */
    percent: (s): number | null => {
      if (!s.total || s.total <= 0 || s.done < 0) return null
      const p = (s.done / s.total) * 100
      return p > 100 ? 100 : Math.round(p)
    },
    /** 面向用户的文案：后端 message 优先，否则按阶段给中文。 */
    label: (s): string => {
      if (s.message) return s.message
      switch (s.stage) {
        case 'download':
          return '正在下载客户端资产'
        case 'verify':
          return '正在校验下载内容'
        case 'extract':
          return '正在解压客户端资产'
        case 'done':
          return '客户端资产已就绪'
        default:
          return ''
      }
    },
  },

  actions: {
    onProgress(e: InstallProgress & { ts?: string }): void {
      this.stage = e.stage ?? ''
      this.subject = e.subject ?? ''
      this.done = e.done ?? 0
      this.total = e.total ?? 0
      this.message = e.message ?? ''
      this.updatedAt = e.ts ?? ''
    },

    reset(): void {
      this.stage = ''
      this.subject = ''
      this.done = 0
      this.total = 0
      this.message = ''
      this.updatedAt = ''
    },
  },
})
