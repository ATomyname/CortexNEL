import { defineStore } from 'pinia'
import * as api from '../api/system'
import type { Settings } from '../api/system'

/** 系统域 store：全局设置，拉取式（EVENTS.md §6 之外的新增域，见文档同步记录）。 */
export const useSystemStore = defineStore('system', {
  state: () => ({
    settings: null as Settings | null,
    loading: false,
    error: '',
  }),

  actions: {
    async refresh(): Promise<void> {
      this.loading = true
      this.error = ''
      try {
        this.settings = await api.getSettings()
      } catch (e) {
        this.error = String(e)
      } finally {
        this.loading = false
      }
    },

    async saveSettings(settings: Settings): Promise<void> {
      await api.saveSettings(settings)
      this.settings = settings
    },

    async saveCredentialOptIn(enabled: boolean): Promise<void> {
      await api.saveCredentialOptIn(enabled)
      await this.refresh()
    },

    async openDataDir(): Promise<void> {
      await api.openDataDir()
    },
  },
})
