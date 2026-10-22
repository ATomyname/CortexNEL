/**
 * 外部链接：官方 Discord 邀请地址与系统浏览器跳转。
 *
 * Wails 桌面容器里 window.open 会被 WebView 吞掉，必须走 runtime 的 BrowserOpenURL；
 * 纯浏览器预览（无 runtime）时退回 window.open，保证两条路径都能跳出去。
 */
import { BrowserOpenURL } from '../../wailsjs/runtime'

/** 官方 Discord 邀请地址（界面与 README 共用这一处，改地址只改这里）。 */
export const DISCORD_INVITE_URL = 'https://discord.gg/pvKnVDnJxN'

/** 用系统默认浏览器打开给定地址。 */
export function openExternal(url: string): void {
  try {
    if (typeof BrowserOpenURL === 'function') {
      BrowserOpenURL(url)
      return
    }
  } catch {
    // 落到下面的 window.open
  }
  window.open(url, '_blank', 'noopener')
}

/** 跳转官方 Discord。 */
export function openDiscord(): void {
  openExternal(DISCORD_INVITE_URL)
}
