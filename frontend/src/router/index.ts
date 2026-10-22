import { createRouter, createWebHashHistory, type RouteRecordRaw } from 'vue-router'

// 哈希模式：Wails 以 file:// 提供静态资源，history 模式会 404。
// 信息架构：概览/代理管理/账号/皮肤 | 网络服务器/租赁服务器 | 影子端/白端组件 | 设置。
const routes: RouteRecordRaw[] = [
  { path: '/', name: 'overview', component: () => import('../views/OverviewView.vue') },
  { path: '/proxies', name: 'proxies', component: () => import('../views/ProxyManagerView.vue') },
  { path: '/accounts', name: 'accounts', component: () => import('../views/AccountsView.vue') },
  { path: '/skins', name: 'skins', component: () => import('../views/SkinsView.vue') },
  { path: '/servers', name: 'servers', component: () => import('../views/ServersView.vue') },
  { path: '/rental', name: 'rental', component: () => import('../views/RentalView.vue') },
  { path: '/shadow', name: 'shadow', component: () => import('../views/ShadowView.vue') },
  { path: '/plugins', name: 'plugins', component: () => import('../views/PluginsView.vue') },
  { path: '/settings', name: 'settings', component: () => import('../views/SettingsView.vue') },
]

export const router = createRouter({
  history: createWebHashHistory(),
  routes,
})
