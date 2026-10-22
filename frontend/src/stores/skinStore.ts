import { defineStore } from 'pinia'
import * as api from '../api/skin'
import type { Skin } from '../api/skin'

export const useSkinStore = defineStore('skin', {
  state: () => ({
    skins: [] as Skin[],
    currentSkin: null as Skin | null,
    loading: false,
    error: '',
    applied: '',
  }),
  actions: {
    async refresh(keyword = ''): Promise<void> {
      this.loading = true
      this.error = ''
      try {
        const [list, cur] = await Promise.all([
          api.listSkins(keyword),
          api.getCurrentSkin().catch(() => null),
        ])
        this.skins = list
        if (cur && cur.entityId) {
          this.currentSkin = cur
        }
      } catch (e) {
        this.error = String(e)
      } finally {
        this.loading = false
      }
    },
    async apply(id: string): Promise<void> {
      this.loading = true
      this.error = ''
      try {
        await api.applySkin(id)
        this.applied = id
        const cur = await api.getCurrentSkin().catch(() => null)
        if (cur && cur.entityId) {
          this.currentSkin = cur
        }
      } catch (e) {
        this.error = String(e)
      } finally {
        this.loading = false
      }
    },
    async randomize(): Promise<void> {
      this.loading = true
      this.error = ''
      try {
        const picked = await api.randomizeSkin()
        if (picked && picked.entityId) {
          this.applied = picked.entityId
          this.currentSkin = picked
        }
      } catch (e) {
        this.error = String(e)
      } finally {
        this.loading = false
      }
    },
  },
})
