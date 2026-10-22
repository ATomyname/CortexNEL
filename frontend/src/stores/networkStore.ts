import { defineStore } from 'pinia'
import * as api from '../api/network'
import type { NetworkAddress, NetworkServer } from '../api/network'

/**
 * 网络服 store：实时网易网络服列表（facts §5.3，x19 网关）。
 * 列表为拉取式；封面图按需经详情接口补齐（有界并发，失败静默占位）。
 */
export const useNetworkStore = defineStore('network', {
  state: () => ({
    servers: [] as NetworkServer[],
    loading: false,
    error: '',
    /** 封面图缓存：entityId → URL（空串表示已尝试且无图）。 */
    images: {} as Record<string, string>,
  }),

  getters: {
    /** 关键字过滤（名称/服务器号，大小写不敏感）。 */
    byKeyword: (s) => (kw: string): NetworkServer[] => {
      const q = kw.trim().toLowerCase()
      if (!q) return s.servers
      return s.servers.filter(
        (v) => v.name.toLowerCase().includes(q) || v.entityId.toLowerCase().includes(q),
      )
    },
  },

  actions: {
    async refresh(): Promise<void> {
      this.loading = true
      this.error = ''
      try {
        this.servers = await api.listNetworkServers()
        // 封面加载有界：只取前 24 个（详情接口逐个调用），并发 3。
        void this.loadImages(this.servers.slice(0, 24).map((s) => s.entityId))
      } catch (e) {
        this.error = String(e)
      } finally {
        this.loading = false
      }
    },

    async loadImages(entityIds: string[]): Promise<void> {
      const queue = entityIds.filter((id) => this.images[id] === undefined)
      const worker = async (): Promise<void> => {
        while (queue.length) {
          const id = queue.shift()
          if (!id) break
          try {
            const d = await api.getNetworkServerDetail(id)
            this.images[id] = d.imageUrl ?? ''
          } catch {
            this.images[id] = ''
          }
        }
      }
      await Promise.all(Array.from({ length: 3 }, worker))
    },

    async resolveAddress(entityId: string): Promise<NetworkAddress> {
      return api.resolveNetworkServerAddress(entityId)
    },
  },
})
