<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import {
  CheckCircleFilled,
  CompassOutlined,
  EyeOutlined,
  ReloadOutlined,
  UserOutlined,
} from '@ant-design/icons-vue'
import type { NetworkServer } from '../api/network'
import { useNetworkStore } from '../stores/networkStore'
import { useAccountStore } from '../stores/accountStore'
import { useProfileStore } from '../stores/profileStore'
import { useSessionStore } from '../stores/sessionStore'
import { useSystemStore } from '../stores/systemStore'
import * as networkApi from '../api/network'
import * as accountApi from '../api/account'
import ServerDetailModal from '../components/ServerDetailModal.vue'
import ServerRoleModal from '../components/ServerRoleModal.vue'

const router = useRouter()
const network = useNetworkStore()
const accounts = useAccountStore()
const profiles = useProfileStore()
const session = useSessionStore()
const system = useSystemStore()

const keyword = ref('')
const busy = ref(false)
const launchMode = ref<'proxy' | 'client'>('client')
const pendingJoin = ref<NetworkServer | null>(null)
const roleOpen = ref(false)
const roles = ref<networkApi.Role[]>([])
const selectedRole = ref('')
const roleError = ref('')
const roleLoading = ref(false)
const detailOpen = ref(false)
const detail = ref<networkApi.NetworkServerDetail | null>(null)
const pageError = ref('')

onMounted(async () => {
  await Promise.all([
    network.refresh(),
    system.refresh(),
    accounts.refresh(),
    profiles.refresh(),
  ])
})

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  const sList = network.servers
  const hideZero = system.settings?.hideZeroPlayers ?? false
  const hideMaint = system.settings?.hideMaintaining ?? false
  const hideBedrock = system.settings?.hideBedrockOnly ?? false

  return sList.filter((s) => {
    if (hideZero && (Number(s.onlineCount) <= 0 || !s.onlineCount || s.onlineCount === '0')) {
      return false
    }
    if (hideMaint && s.isMaintaining) {
      return false
    }
    if (hideBedrock && s.isBedrockOnly) {
      return false
    }
    if (kw && !s.name.toLowerCase().includes(kw) && !s.entityId.toLowerCase().includes(kw)) {
      return false
    }
    return true
  })
})

const filteredCount = computed(() => network.servers.length - filtered.value.length)

async function toggleFilter(key: 'hideZeroPlayers' | 'hideMaintaining' | 'hideBedrockOnly'): Promise<void> {
  if (!system.settings) return
  const next = { ...system.settings, [key]: !system.settings[key] }
  await system.saveSettings(next)
}

/** 清理 HTML 标签供单行/两行简介展示 */
function cleanSummary(text?: string): string {
  if (!text) return '官方推荐 Minecraft 网络服，支持多种特色玩法。'
  return text.replace(/<[^>]*>?/gm, '').trim() || '官方推荐 Minecraft 网络服，支持多种特色玩法。'
}

/** 示例延迟（协议未提供该字段）：由 entityId 稳定派生，仅用于版面演示。 */
function latencyOf(entityId: string): number {
  let sum = 0
  for (const ch of entityId) sum = (sum + ch.charCodeAt(0)) % 997
  return 24 + (sum % 60)
}

/** 检查某个服务器是否是最近选中的主服务器 */
function isMainServer(s: NetworkServer): boolean {
  const targetId = 'srv-' + s.entityId
  return system.settings?.mainServerId === targetId
}

/** 设为主服务器（解析地址 -> 保存档案 -> 写入全局配置） */
async function setAsMain(s: NetworkServer): Promise<void> {
  pageError.value = ''
  busy.value = true
  try {
    const addr = await network.resolveAddress(s.entityId)
    const id = 'srv-' + s.entityId
    let version = s.mcVersion || ''
    if (!version) {
      try {
        const d = await networkApi.getNetworkServerDetail(s.entityId)
        version = d.mcVersion || ''
      } catch {}
    }
    if (!version) version = '1.20'
    await profiles.saveServer({
      id,
      name: s.name,
      gameId: s.entityId,
      host: addr.host,
      port: addr.port,
      mcVersion: version,
      isRental: false,
    })
    if (system.settings) {
      await system.saveSettings({ ...system.settings, mainServerId: id })
    }
  } catch (e) {
    pageError.value = '更新服务器信息失败: ' + String(e)
  } finally {
    busy.value = false
  }
}

/** 进入服务器：查询/选择角色，准备进入 */
async function enterServer(s: NetworkServer): Promise<void> {
  pageError.value = ''
  if (s.isMaintaining) {
    message.warning('该服务器正在停服维护中，暂无法进入。')
    return
  }
  if (s.isBedrockOnly) {
    message.warning('此服务器为网易基岩版独立服务器（Apollo C++），暂不支持 Java 版客户端直接接入。')
    return
  }
  if (!accounts.accounts.length) {
    network.error = '请先到「账号管理」登录游戏账号后再进入服务器'
    return
  }
  pendingJoin.value = s
  roleError.value = ''
  roleLoading.value = true
  busy.value = true
  try {
    roles.value = await networkApi.listRoles(s.entityId)
    selectedRole.value = roles.value[0]?.name ?? ''
    roleOpen.value = true
  } catch (e) {
    pageError.value = '获取服务器角色失败: ' + String(e)
  } finally {
    roleLoading.value = false
    busy.value = false
  }
}

/** 角色弹窗点"创建"：落库后刷新角色列表并选中新角色 */
async function createRole(name: string): Promise<void> {
  if (!pendingJoin.value || !name) return
  roleError.value = ''
  roleLoading.value = true
  try {
    await networkApi.createRole(pendingJoin.value.entityId, name)
    roles.value = await networkApi.listRoles(pendingJoin.value.entityId)
    selectedRole.value = name
    message.success('角色创建成功')
  } catch (e) {
    roleError.value = String(e)
  } finally {
    roleLoading.value = false
  }
}

async function handleLaunch(mode: 'proxy' | 'client'): Promise<void> {
  const s = pendingJoin.value
  if (!s || !selectedRole.value) {
    roleError.value = '请选择或创建一个角色'
    return
  }
  launchMode.value = mode
  busy.value = true
  roleError.value = ''
  try {
    const account = accounts.accounts.find((v) => v.id === system.settings?.mainAccountId) ?? accounts.accounts[0]
    await accountApi.updateAccountRoleName(account.id, selectedRole.value)
    if (system.settings && system.settings.mainAccountId !== account.id) {
      await system.saveSettings({ ...system.settings, mainAccountId: account.id })
    }
    await setAsMain(s)
    const targetId = 'srv-' + s.entityId

    roleOpen.value = false
    if (mode === 'proxy') {      const id = await session.startWithTarget(targetId, account.id, false)
      const addr = session.sessions.find((x) => x.sessionId === id)?.listenAddr
      message.success(`已启动代理监听 (${addr || '127.0.0.1:25565'})，可在已运行客户端中直连`)
    } else {
      await session.startWithTarget(targetId, account.id, true)
      message.success('已启动 Minecraft 客户端并连入中继代理')
    }
  } catch (e) {
    pageError.value = '启动失败: ' + String(e)
    roleError.value = String(e)
  } finally {
    busy.value = false
  }
}

async function showDetail(s: NetworkServer): Promise<void> {
  pageError.value = ''
  busy.value = true
  try {
    detail.value = await networkApi.getNetworkServerDetail(s.entityId)
    detailOpen.value = true
  } catch (e) {
    pageError.value = '获取服务器详情失败: ' + String(e)
  } finally {
    busy.value = false
  }
}

/** 详情弹窗的"进入此服务器"：用详情对象构造进服目标 */
function enterFromDetail(): void {
  const d = detail.value
  if (!d) return
  detailOpen.value = false
  void enterServer({
    entityId: d.entityId,
    name: d.name,
    onlineCount: '',
    briefSummary: '',
    imageUrl: d.imageUrl,
    mcVersion: d.mcVersion,
    isBedrockOnly: d.isBedrockOnly,
    isMaintaining: d.isMaintaining,
  })
}
</script>

<template>
  <section class="page">
    <header class="page-header">
      <div>
        <h1 class="page-title">网络服务器</h1>
        <p class="page-desc">（java服务器）网易官方与社区热门 Minecraft 网络服列表</p>
      </div>
      <a-space>
        <a-input-search
          v-model:value="keyword"
          allow-clear
          placeholder="搜索服务器名称 / 服务器号..."
          style="width: 280px"
        />
        <a-button :loading="network.loading" @click="network.refresh()">
          <template #icon><ReloadOutlined /></template>
          刷新
        </a-button>
      </a-space>
    </header>

    <a-alert
      v-if="network.error"
      type="error"
      show-icon
      closable
      class="mb"
      @close="network.refresh(); network.error = ''"
    >
      <template #message>
        <span>{{ network.error }}</span>
        <a-button v-if="network.error.includes('未登录') || !accounts.accounts.length" type="link" size="small" @click="router.push('/accounts')">
          去账号管理登录 →
        </a-button>
        <a-button type="link" size="small" @click="network.refresh()">重试</a-button>
      </template>
    </a-alert>

    <a-alert v-if="pageError" type="warning" show-icon closable class="mb" :message="pageError" @close="pageError = ''" />

    <!-- 服务器卡片网格 -->
    <a-card :bordered="false" class="main-card">
      <template #title><span class="card-title">服务器列表</span></template>
      <template #extra>
        <a-space :size="8" wrap>
          <a-checkable-tag
            :checked="system.settings?.hideZeroPlayers ?? false"
            @change="toggleFilter('hideZeroPlayers')"
          >
            隐藏 0 人
          </a-checkable-tag>
          <a-checkable-tag
            :checked="system.settings?.hideMaintaining ?? false"
            @change="toggleFilter('hideMaintaining')"
          >
            隐藏维护
          </a-checkable-tag>
          <a-checkable-tag
            :checked="system.settings?.hideBedrockOnly ?? false"
            @change="toggleFilter('hideBedrockOnly')"
          >
            隐藏基岩版
          </a-checkable-tag>
          <a-tag color="blue">共 {{ network.servers.length }} 个网络服</a-tag>
          <span v-if="filteredCount > 0" class="search-hint">（已过滤 {{ filteredCount }} 项）</span>
        </a-space>
      </template>

      <a-spin :spinning="network.loading">
        <div v-if="filtered.length" class="srv-grid">
          <div
            v-for="server in filtered"
            :key="server.entityId"
            class="srv-card"
            :class="{ 'is-main': isMainServer(server) }"
          >
            <!-- 封面图与浮动徽标 -->
            <div class="srv-cover-wrap">
              <img
                v-if="server.imageUrl"
                :src="server.imageUrl"
                :alt="server.name"
                class="srv-cover-img"
                loading="lazy"
              />
              <div v-else class="srv-cover-placeholder">
                <span class="placeholder-tag">Minecraft</span>
                <span class="placeholder-name">{{ server.name }}</span>
              </div>

              <!-- 顶部浮动徽标 -->
              <div class="cover-top-bar">
                <a-tag v-if="isMainServer(server)" color="success" class="badge-main">
                  <template #icon><CheckCircleFilled /></template>
                  当前主服
                </a-tag>
                <div v-else class="badge-status-pill">
                  <span class="status-dot"></span>
                  <span>在线</span>
                </div>

                <div class="badge-online-chip">
                  <UserOutlined />
                  <span>{{ server.onlineCount || 0 }}</span>
                </div>
              </div>
            </div>

            <!-- 卡片信息主体 -->
            <div class="srv-card-body">
              <div class="srv-title-line">
                <h3 class="srv-title" :title="server.name">{{ server.name }}</h3>
              </div>

              <div class="srv-meta-line">
                <code class="srv-id">#{{ server.entityId }}</code>
                <a-tag v-if="server.isMaintaining" color="error" class="srv-version-tag">维护中</a-tag>
                <a-tag v-else-if="server.isBedrockOnly" color="warning" class="srv-version-tag">基岩版</a-tag>
                <a-tag v-else color="processing" class="srv-version-tag">{{ server.mcVersion || '1.20' }}</a-tag>
                <span class="srv-latency" :class="latencyOf(server.entityId) < 50 ? 'lat-good' : 'lat-normal'">
                  {{ latencyOf(server.entityId) }} ms
                </span>
              </div>

              <p class="srv-desc" :title="cleanSummary(server.briefSummary)">
                {{ cleanSummary(server.briefSummary) }}
              </p>
            </div>

            <!-- 底部操作区域 -->
            <div class="srv-card-footer">
              <a-tooltip v-if="server.isMaintaining" title="该服务器正在停服维护中">
                <a-button
                  type="primary"
                  size="small"
                  class="btn-enter"
                  disabled
                >
                  <template #icon><CompassOutlined /></template>
                  维护中
                </a-button>
              </a-tooltip>
              <a-tooltip v-else-if="server.isBedrockOnly" title="此服务器为纯基岩版，暂不支持 Java 客户端直接进入">
                <a-button
                  type="default"
                  size="small"
                  class="btn-enter"
                  :disabled="busy"
                  @click="enterServer(server)"
                >
                  <template #icon><CompassOutlined /></template>
                  基岩服
                </a-button>
              </a-tooltip>
              <a-button
                v-else
                type="primary"
                size="small"
                class="btn-enter"
                :disabled="busy"
                @click="enterServer(server)"
              >
                <template #icon><CompassOutlined /></template>
                进入服务器
              </a-button>

              <a-tooltip title="查看图文详细介绍">
                <a-button size="small" :disabled="busy" @click="showDetail(server)">
                  <template #icon><EyeOutlined /></template>
                </a-button>
              </a-tooltip>
            </div>
          </div>
        </div>

        <a-empty
          v-else-if="!network.loading"
          :description="keyword ? `没有匹配「${keyword}」的服务器，请尝试更换关键词。` : '暂无网络服务器数据，请确保已登录账号并检查网络连接。若反复如此，可在「设置」中开启「绕过系统代理」后重试。'"
          class="empty-panel"
        />
      </a-spin>
    </a-card>

    <ServerDetailModal
      :open="detailOpen"
      :detail="detail"
      :busy="busy"
      :mirror-running="!!session.mirrorSession"
      @cancel="detailOpen = false"
      @enter="enterFromDetail"
    />

    <ServerRoleModal
      :open="roleOpen"
      :server-name="pendingJoin?.name ?? ''"
      :roles="roles"
      :selected-role="selectedRole"
      :role-error="roleError"
      :loading="roleLoading"
      :busy="busy"
      :launch-mode="launchMode"
      :mirror-running="!!session.mirrorSession"
      @cancel="roleOpen = false"
      @update:selected-role="selectedRole = $event"
      @update:role-error="roleError = $event"
      @create="createRole"
      @launch="handleLaunch"
    />
  </section>
</template>

<style scoped>
.main-card {
  box-shadow: var(--n-card-shadow);
}

.search-hint {
  font-size: 12px;
  color: var(--n-text-3);
}

.empty-panel {
  padding: 40px 0;
}

/* 紧凑卡片网格 */
.srv-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(250px, 1fr));
  gap: 14px;
}

.srv-card {
  display: flex;
  flex-direction: column;
  background: var(--n-bg-card);
  border: 1px solid var(--n-separator);
  border-radius: var(--n-radius);
  overflow: hidden;
  transition: transform var(--n-trans), box-shadow var(--n-trans), border-color var(--n-trans);
}

.srv-card:hover {
  transform: translateY(-2px);
  box-shadow: var(--n-card-shadow);
  border-color: rgba(0, 122, 255, 0.35);
}

.srv-card.is-main {
  border-color: var(--n-blue);
  box-shadow: 0 0 0 1px var(--n-blue), var(--n-card-shadow);
}

/* 封面区 */
.srv-cover-wrap {
  position: relative;
  width: 100%;
  height: 105px;
  background: var(--n-terminal-bg);
  overflow: hidden;
}

.srv-cover-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.srv-cover-placeholder {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  background: var(--n-terminal-bg);
  color: var(--n-terminal-text);
  padding: 10px;
  text-align: center;
}

.placeholder-tag {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.5px;
  color: var(--n-blue);
  background: var(--n-blue-tint);
  padding: 1px 6px;
  border-radius: 4px;
}

.placeholder-name {
  font-size: 12.5px;
  font-weight: 600;
  max-width: 90%;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.cover-top-bar {
  position: absolute;
  top: 8px;
  left: 8px;
  right: 8px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  pointer-events: none;
}

.badge-main {
  margin: 0;
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.3);
}

.badge-status-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 10px;
  background: var(--n-scrim);
  backdrop-filter: blur(8px);
  color: var(--n-terminal-text);
}

.badge-status-pill .status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--n-green);
}

.badge-online-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 10px;
  background: var(--n-scrim);
  backdrop-filter: blur(8px);
  color: var(--n-terminal-text);
}

/* 卡片内容 */
.srv-card-body {
  padding: 10px 12px;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.srv-title-line {
  display: flex;
  align-items: center;
}

.srv-title {
  font-size: 13.5px;
  font-weight: 600;
  margin: 0;
  color: var(--n-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.srv-meta-line {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.srv-id {
  font-family: var(--n-mono);
  font-size: 11px;
  color: var(--n-text-3);
  max-width: 90px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.srv-version-tag {
  font-size: 10.5px;
  padding: 0 4px;
  margin: 0;
}

.srv-latency {
  font-family: var(--n-mono);
  font-size: 11px;
  font-weight: 600;
  margin-left: auto;
}

.lat-good {
  color: var(--n-green);
}

.lat-normal {
  color: var(--n-orange);
}

.srv-desc {
  font-size: 12px;
  color: var(--n-text-2);
  line-height: 1.45;
  margin: 0;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  min-height: 34px;
}

/* 底部操作区 */
.srv-card-footer {
  padding: 8px 12px;
  border-top: 1px solid var(--n-separator);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  background: var(--n-surface-subtle);
}

.btn-enter {
  flex: 1;
}
</style>
