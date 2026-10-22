/**
 * 主题状态：Vue 响应式单例 + 根元素 class（styles/tokens.css 变量切换）。
 * 选择持久化到 localStorage；ConfigProvider 的 antd 主题经 App.vue 订阅此状态。
 * 浅色为主（Apple HIG），无记录时默认浅色。
 */
import { ref } from 'vue'
import { migrateLegacyStorageKeys } from './storageMigration'

// 键名迁移必须早于本模块对 KEY 的读取：App.vue 的 <script setup> 顶层就会调用
// initTheme()，而 ES module 的导入先于 import 方的函数体求值——放在 main.ts 里
// 已经晚了。本模块在 App.vue 的导入链上，模块顶层执行即满足时序；函数幂等。
migrateLegacyStorageKeys()

const KEY = 'cortexnel.theme'
export type ThemeName = 'light' | 'dark'

const current = ref<ThemeName>('light')

function apply(name: ThemeName): void {
  const root = document.documentElement
  root.classList.remove('theme-light', 'theme-dark')
  root.classList.add(`theme-${name}`)
}

/** 应用启动时恢复上次选择。 */
export function initTheme(): ThemeName {
  const saved = localStorage.getItem(KEY)
  const name: ThemeName = saved === 'dark' || saved === 'light' ? saved : 'light'
  current.value = name
  apply(name)
  return name
}

export function currentTheme(): ThemeName {
  return current.value
}

/** 供 App.vue 订阅的响应式主题名。 */
export function themeRef() {
  return current
}

export function setTheme(name: ThemeName): void {
  current.value = name
  apply(name)
  localStorage.setItem(KEY, name)
}

export function toggleTheme(): ThemeName {
  const next: ThemeName = current.value === 'dark' ? 'light' : 'dark'
  setTheme(next)
  return next
}
