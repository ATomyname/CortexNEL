import { defineStore } from 'pinia'
import * as api from '../api/rental'
import type { RentalServer } from '../api/rental'
export const useRentalStore = defineStore('rental', {
  state: () => ({ servers: [] as RentalServer[], loading: false, error: '' }),
  actions: { async refresh(): Promise<void> { this.loading = true; this.error = ''; try { this.servers = await api.listRentalServers() } catch (e) { this.error = String(e) } finally { this.loading = false } } },
})
