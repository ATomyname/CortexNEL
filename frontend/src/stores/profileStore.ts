import { defineStore } from 'pinia'
import * as api from '../api/profile'
import type { Env, Server } from '../api/profile'

/** 档案域 store：服务器 / 环境，全部为拉取式绑定调用（EVENTS.md §6）。 */
export const useProfileStore = defineStore('profile', {
  state: () => ({
    servers: [] as Server[],
    envs: [] as Env[],
    loading: false,
    error: '',
  }),

  actions: {
    async refresh(): Promise<void> {
      this.loading = true
      this.error = ''
      try {
        const [servers, envs] = await Promise.all([api.listServers(), api.listEnvs()])
        this.servers = servers
        this.envs = envs
      } catch (e) {
        this.error = String(e)
      } finally {
        this.loading = false
      }
    },

    async saveServer(server: Server): Promise<void> {
      await api.saveServer(server)
      await this.refresh()
    },

    async removeServer(id: string): Promise<void> {
      await api.removeServer(id)
      await this.refresh()
    },

    async saveEnv(env: Env): Promise<void> {
      await api.saveEnv(env)
      await this.refresh()
    },

    async removeEnv(id: string): Promise<void> {
      await api.removeEnv(id)
      await this.refresh()
    },
  },
})
