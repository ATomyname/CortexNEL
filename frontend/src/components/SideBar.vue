<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ApiOutlined,
  BulbFilled,
  BulbOutlined,
  CloudOutlined,
  ClusterOutlined,
  EyeOutlined,
  HomeOutlined,
  SettingOutlined,
  SkinOutlined,
  TeamOutlined,
  UserOutlined,
} from '@ant-design/icons-vue'
import StatusBadge from './StatusBadge.vue'
import { toggleTheme, themeRef } from '../lib/theme'
import { openDiscord } from '../lib/external'
import { useSessionStore } from '../stores/sessionStore'

const route = useRoute()
const router = useRouter()
const session = useSessionStore()
const theme = themeRef()

// 信息架构（架构文档 §10.3）：概览/账号/皮肤 ‖ 网络服务器/租赁服务器 ‖ 中继与数据包/组件与插件 ‖ 设置。
const selectedKeys = computed(() => [route.path])

function go(path: string): void {
  if (route.path !== path) void router.push(path)
}

function onToggleTheme(): void {
  toggleTheme()
}

/** 侧栏入口直接跳官方 Discord（邀请地址是 URL，没有"群号可复制"这一步）。 */
function onOpenDiscord(): void {
  openDiscord()
}
</script>

<template>
  <aside class="side-bar glass">
    <div class="brand-area">
      <div class="brand-logo">
        <img src="/appicon.png" alt="CortexNEL Logo" class="logo-image" />
        <div class="brand-meta">
          <span class="brand-name">CortexNEL</span>
        </div>
      </div>
      <div class="session-quick-status">
        <StatusBadge
          v-if="session.current"
          :stage="session.current.stage || 'IDLE'"
          :active="true"
          size="sm"
        />
        <StatusBadge v-else stage="IDLE" :active="false" size="sm" />
      </div>
    </div>

    <a-menu mode="inline" :selected-keys="selectedKeys" class="side-menu" @click="go($event.key as string)">
      <a-menu-item key="/">
        <template #icon><HomeOutlined /></template>
        <span>概览</span>
      </a-menu-item>
      <a-menu-item key="/proxies">
        <template #icon><ApiOutlined /></template>
        <span>代理管理</span>
      </a-menu-item>
      <a-menu-item key="/accounts">
        <template #icon><UserOutlined /></template>
        <span>账号管理</span>
      </a-menu-item>
      <a-menu-item key="/skins">
        <template #icon><SkinOutlined /></template>
        <span>皮肤</span>
      </a-menu-item>
      <a-menu-item-group title="（java服务器）">
        <a-menu-item key="/servers">
          <template #icon><ClusterOutlined /></template>
          <span>网络服务器</span>
        </a-menu-item>
        <a-menu-item key="/rental">
          <template #icon><CloudOutlined /></template>
          <span>租赁服务器</span>
        </a-menu-item>
      </a-menu-item-group>
      <a-menu-item-group title="扩展与配置">
        <a-menu-item key="/shadow">
          <template #icon><EyeOutlined /></template>
          <span>影子端</span>
        </a-menu-item>
        <a-menu-item key="/plugins">
          <template #icon><ApiOutlined /></template>
          <span>白端设置</span>
        </a-menu-item>
        <a-menu-item key="/settings">
          <template #icon><SettingOutlined /></template>
          <span>系统设置</span>
        </a-menu-item>
      </a-menu-item-group>
    </a-menu>

    <div class="side-footer">
      <a-button block class="group-btn" @click="onOpenDiscord">
        <template #icon><TeamOutlined /></template>
        官方 Discord
      </a-button>
      <a-button block @click="onToggleTheme">
        <template #icon>
          <BulbFilled v-if="theme === 'dark'" />
          <BulbOutlined v-else />
        </template>
        {{ theme === 'dark' ? '切换到亮色模式' : '切换到暗色模式' }}
      </a-button>
    </div>
  </aside>
</template>

<style scoped>
.side-bar {
  display: flex;
  flex-direction: column;
  width: 224px;
  flex-shrink: 0;
  padding: 16px 12px 14px;
  border-right: 1px solid var(--n-separator);
  user-select: none;
}

.brand-area {
  padding: 4px 8px 14px;
  border-bottom: 1px solid var(--n-separator);
  margin-bottom: 10px;
}

.brand-logo {
  display: flex;
  align-items: center;
  gap: 10px;
}

.logo-image {
  width: 32px;
  height: 32px;
  border-radius: 9px;
  object-fit: cover;
  box-shadow: 0 2px 6px rgba(0, 122, 255, 0.25);
}

.brand-meta {
  display: flex;
  flex-direction: column;
}

.brand-name {
  font-size: 16px;
  font-weight: 700;
  color: var(--n-text);
  line-height: 1.25;
  letter-spacing: -0.01em;
}

.session-quick-status {
  margin-top: 10px;
}

.side-menu {
  flex: 1;
  overflow-y: auto;
  border-inline-end: none !important;
  background: transparent !important;
}

.side-menu :deep(.ant-menu-item) {
  height: 38px;
  line-height: 38px;
  margin-inline: 6px;
  width: calc(100% - 12px);
  border-radius: 8px;
  transition: all var(--n-motion);
}

.side-menu :deep(.ant-menu-item-selected) {
  background: var(--n-blue-tint);
  font-weight: 600;
}

.theme-dark .side-menu :deep(.ant-menu-item-selected) {
  background: var(--n-blue-tint);
}

.side-menu :deep(.ant-menu-item-group-title) {
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.05em;
  color: var(--n-text-3);
  text-transform: uppercase;
  padding-top: 14px;
  padding-bottom: 4px;
}

.side-footer {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-top: 12px;
  border-top: 1px solid var(--n-separator);
}

.group-btn {
  color: var(--n-blue, #1677ff);
  font-weight: 500;
}
</style>
