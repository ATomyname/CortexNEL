// 账号域薄封装（架构文档 §10.2）：统一走 Wails 生成绑定，组件禁止散落调用。
import * as binding from '../../wailsjs/go/app/App'
import { app } from '../../wailsjs/go/models'

export type Account = app.AccountDTO

export function login(label: string, cookie: string, persistCred: boolean): Promise<Account> {
  return binding.Login(label, cookie, persistCred)
}

/** 导入小号机复制的 Channel/账号/密码/sauth_json 文本。 */
export function importSmallMachine(raw: string, persistCred: boolean): Promise<Account> {
  return binding.ImportSmallMachine(raw, persistCred)
}

export function listAccounts(): Promise<Account[]> {
  return binding.ListAccounts()
}

export function switchAccount(id: string): Promise<void> {
  return binding.SwitchAccount(id)
}

export function logout(id: string): Promise<void> {
  return binding.Logout(id)
}

export function updateAccountLabel(id: string, label: string): Promise<void> {
  return binding.UpdateAccountLabel(id, label)
}

export function updateAccountRoleName(id: string, roleName: string): Promise<void> {
  return binding.UpdateAccountRoleName(id, roleName)
}

/** 4399 密码登录（后端自动识别验证码，facts §6.1.1）。 */
export function login4399(account: string, password: string, label: string, persist: boolean): Promise<Account> {
  return binding.Login4399(account, password, label, persist)
}

/** x19 邮箱账号密码登录（mPay 设备 + 邮箱链，facts §6.3）。 */
export function loginX19(email: string, password: string, label: string, persist: boolean): Promise<Account> {
  return binding.LoginX19(email, password, label, persist)
}

export function requestX19SMS(phone: string): Promise<void> {
  return binding.RequestX19SMS(phone)
}

export function loginX19PhonePassword(phone: string, password: string, label: string, persist: boolean): Promise<Account> {
  return binding.LoginX19PhonePassword(phone, password, label, persist)
}

export function loginX19PhoneSMS(phone: string, code: string, label: string, persist: boolean): Promise<Account> {
  return binding.LoginX19PhoneSMS(phone, code, label, persist)
}

/** 触发静默自动重新登录（使用保存的凭据换取新的 session token）。 */
export function reLogin(id: string): Promise<Account> {
  return binding.ReLogin(id)
}
