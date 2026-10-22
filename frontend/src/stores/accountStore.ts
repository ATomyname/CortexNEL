import { defineStore } from 'pinia'
import * as api from '../api/account'
import type { Account } from '../api/account'

/**
 * 账号域 store：消费 account.login / account.token.expired 事件（bus.ts），
 * 列表为拉取式绑定调用（EVENTS.md §6）。
 */
export const useAccountStore = defineStore('account', {
  state: () => ({
    accounts: [] as Account[],
    /** 当前主账号 ID（来自全局设置）。 */
    mainAccountID: '',
    loading: false,
    /** 最近一次操作的错误（用户可读文案，S1）。 */
    error: '',
  }),

  getters: {
    hasAccount: (s) => s.accounts.length > 0,
  },

  actions: {
    async refresh(): Promise<void> {
      this.loading = true
      this.error = ''
      try {
        this.accounts = await api.listAccounts()
      } catch (e) {
        this.error = String(e)
      } finally {
        this.loading = false
      }
    },

    async login(label: string, cookie: string, persistCred: boolean): Promise<void> {
      this.loading = true
      this.error = ''
      try {
        await api.login(label, cookie, persistCred)
        await this.refresh()
        if (!this.mainAccountID && this.accounts[0]) await this.switchTo(this.accounts[0].id)
      } catch (e) {
        this.error = String(e)
        throw e
      } finally {
        this.loading = false
      }
    },

    async importSmallMachine(raw: string, persistCred: boolean): Promise<void> {
      this.loading = true
      this.error = ''
      try {
        await api.importSmallMachine(raw, persistCred)
        await this.refresh()
        if (!this.mainAccountID && this.accounts[0]) await this.switchTo(this.accounts[0].id)
      } catch (e) {
        this.error = String(e)
        throw e
      } finally {
        this.loading = false
      }
    },

    /** 4399 账号密码登录（后端自动识别验证码并走既有 Cookie 收尾）。 */
    async login4399(account: string, password: string, label: string, persistCred: boolean): Promise<void> {
      this.loading = true
      this.error = ''
      try {
        await api.login4399(account, password, label, persistCred)
        await this.refresh()
        if (!this.mainAccountID && this.accounts[0]) await this.switchTo(this.accounts[0].id)
      } catch (e) {
        this.error = String(e)
        throw e
      } finally {
        this.loading = false
      }
    },

    /** x19 邮箱账号密码登录（facts §6.3：mPay 设备 + 邮箱链 + 收尾链）。 */
    async loginX19(email: string, password: string, label: string, persistCred: boolean): Promise<void> {
      this.loading = true
      this.error = ''
      try {
        await api.loginX19(email, password, label, persistCred)
        await this.refresh()
        if (!this.mainAccountID && this.accounts[0]) await this.switchTo(this.accounts[0].id)
      } catch (e) {
        this.error = String(e)
        throw e
      } finally {
        this.loading = false
      }
    },

    async requestX19SMS(phone: string): Promise<void> {
      await api.requestX19SMS(phone)
    },

    async loginX19PhonePassword(phone: string, password: string, label: string, persistCred: boolean): Promise<void> {
      this.loading = true
      this.error = ''
      try {
        await api.loginX19PhonePassword(phone, password, label, persistCred)
        await this.refresh()
        if (!this.mainAccountID && this.accounts[0]) await this.switchTo(this.accounts[0].id)
      } catch (e) {
        this.error = String(e)
        throw e
      } finally {
        this.loading = false
      }
    },

    async loginX19PhoneSMS(phone: string, code: string, label: string, persistCred: boolean): Promise<void> {
      this.loading = true
      this.error = ''
      try {
        await api.loginX19PhoneSMS(phone, code, label, persistCred)
        await this.refresh()
        if (!this.mainAccountID && this.accounts[0]) await this.switchTo(this.accounts[0].id)
      } catch (e) {
        this.error = String(e)
        throw e
      } finally {
        this.loading = false
      }
    },

    async switchTo(id: string): Promise<void> {
      await api.switchAccount(id)
      this.mainAccountID = id
    },

    async logout(id: string): Promise<void> {
      await api.logout(id)
      if (this.mainAccountID === id) {
        this.mainAccountID = ''
      }
      await this.refresh()
      if (!this.mainAccountID && this.accounts[0]) await this.switchTo(this.accounts[0].id)
    },

    async reLogin(id: string): Promise<void> {
      this.loading = true
      this.error = ''
      try {
        await api.reLogin(id)
        await this.refresh()
      } catch (e) {
        this.error = String(e)
        throw e
      } finally {
        this.loading = false
      }
    },
  },
})
