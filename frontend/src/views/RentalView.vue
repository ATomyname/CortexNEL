<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import {
  ApiOutlined,
  CloudOutlined,
  EyeOutlined,
  LockOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  UserOutlined,
} from '@ant-design/icons-vue'
import type { RentalServer, RentalRole } from '../api/rental'
import * as rentalApi from '../api/rental'
import * as accountApi from '../api/account'
import { useRentalStore } from '../stores/rentalStore'
import { useAccountStore } from '../stores/accountStore'
import { useProfileStore } from '../stores/profileStore'
import { useSystemStore } from '../stores/systemStore'
import { popupContainer } from '../lib/popupContainer'
import { useSessionStore } from '../stores/sessionStore'

const rental = useRentalStore()
const accounts = useAccountStore()
const profiles = useProfileStore()
const system = useSystemStore()
const session = useSessionStore()

const keyword = ref('')
const detail = ref<RentalServer | null>(null)
const detailLoading = ref(false)
const roles = ref<RentalRole[]>([])
const newRole = ref('')
const roleError = ref('')
const password = ref('')
const selectedRole = ref('')
const joinError = ref('')
const joinBusy = ref(false)
const launchMode = ref<'proxy' | 'client'>('client')

function cleanSummary(text?: string): string {
  if (!text) return '暂无详细简介。'
  return text.replace(/<[^>]*>?/gm, '').trim() || '暂无详细简介。'
}

const filtered = computed(() => {
  const key = keyword.value.trim().toLowerCase()
  const hideZero = system.settings?.hideZeroPlayers ?? false
  return rental.servers.filter((s) => {
    if (hideZero && (Number(s.playerCount) <= 0 || !s.playerCount)) {
      return false
    }
    if (key && !`${s.name} ${s.serverName} ${s.entityId}`.toLowerCase().includes(key)) {
      return false
    }
    return true
  })
})

const filteredCount = computed(() => rental.servers.length - filtered.value.length)

async function toggleFilter(key: 'hideZeroPlayers'): Promise<void> {
  if (!system.settings) return
  const next = { ...system.settings, [key]: !system.settings[key] }
  await system.saveSettings(next)
}

onMounted(() => {
  void rental.refresh()
  void system.refresh()
})

async function showDetail(server: RentalServer): Promise<void> {
  detailLoading.value = true
  roleError.value = ''
  selectedRole.value = ''
  password.value = ''
  try {
    const result = await Promise.all([
      rentalApi.getRentalServerDetail(server.entityId),
      rentalApi.listRentalRoles(server.entityId),
    ])
    detail.value = result[0]
    roles.value = result[1]
    selectedRole.value = roles.value[0]?.name ?? ''
  } catch (e) {
    rental.error = String(e)
  } finally {
    detailLoading.value = false
  }
}

async function createRole(): Promise<void> {
  if (!detail.value || !newRole.value.trim()) return
  roleError.value = ''
  try {
    await rentalApi.createRentalRole(detail.value.entityId, newRole.value.trim())
    roles.value = await rentalApi.listRentalRoles(detail.value.entityId)
    newRole.value = ''
  } catch (e) {
    roleError.value = String(e)
  }
}

async function handleRentalLaunch(mode: 'proxy' | 'client'): Promise<void> {
  joinError.value = ''
  if (!detail.value || !accounts.accounts.length) {
    joinError.value = '请先登录游戏账号'
    return
  }
  if (!selectedRole.value) {
    joinError.value = '请先创建或选择角色'
    return
  }
  launchMode.value = mode
  joinBusy.value = true
  try {
    const addr = await rentalApi.resolveRentalServerAddress(detail.value.entityId, password.value)
    const id = 'rental-' + detail.value.entityId
    await profiles.saveServer({
      id,
      name: detail.value.name || detail.value.serverName,
      gameId: detail.value.entityId,
      host: addr.host,
      port: addr.port,
      mcVersion: detail.value.mcVersion || '1.12.2',
      isRental: true,
    })
    const account = accounts.accounts.find((v) => v.id === system.settings?.mainAccountId) ?? accounts.accounts[0]
    await accountApi.updateAccountRoleName(account.id, selectedRole.value)
    if (system.settings) {
      await system.saveSettings({ ...system.settings, mainAccountId: account.id, mainServerId: id })
    }
    detail.value = null

    if (mode === 'proxy') {
      const newId = await session.startWithTarget(id, account.id, false)
      const listenAddr = session.sessions.find((x) => x.sessionId === newId)?.listenAddr
      message.success(`已启动代理监听 (${listenAddr || '127.0.0.1:25565'})，可在已运行客户端中直连`)
    } else {
      await session.startWithTarget(id, account.id, true)
      message.success('已启动 Minecraft 客户端并连入中继代理')
    }
  } catch (e) {
    joinError.value = String(e)
  } finally {
    joinBusy.value = false
  }
}
</script>

<template>
  <section class="page">
    <header class="page-header">
      <div>
        <h1 class="page-title">租赁服务器</h1>
        <p class="page-desc">浏览公开租赁服，查看协议版本、在线人数并快捷进入</p>
      </div>
      <a-space>
        <a-input-search
          v-model:value="keyword"
          placeholder="搜索名称 / 服务器号..."
          style="width: 260px"
          allow-clear
        />
        <a-button :loading="rental.loading" @click="rental.refresh()">
          <template #icon><ReloadOutlined /></template>
          刷新
        </a-button>
      </a-space>
    </header>

    <a-alert v-if="rental.error" type="error" show-icon closable class="mb" :message="rental.error" @close="rental.error = ''" />

    <a-card :bordered="false" class="rental-panel">
      <template #title><span class="card-title">租赁服列表</span></template>
      <template #extra>
        <a-space :size="8" wrap>
          <a-checkable-tag
            :checked="system.settings?.hideZeroPlayers ?? false"
            @change="toggleFilter('hideZeroPlayers')"
          >
            隐藏 0 人
          </a-checkable-tag>
          <a-tag color="blue">共 {{ rental.servers.length }} 个租赁服</a-tag>
          <span v-if="filteredCount > 0" class="search-hint">（已过滤 {{ filteredCount }} 项）</span>
        </a-space>
      </template>

      <div v-if="rental.loading" class="center-spin">
        <a-spin tip="正在加载租赁服列表..." />
      </div>

      <a-empty v-else-if="!filtered.length" description="暂无公开租赁服数据" />

      <div v-else class="rental-grid">
        <div
          v-for="server in filtered"
          :key="server.entityId"
          class="rental-card"
        >
          <!-- 封面图与浮动徽标 -->
          <div class="rental-cover-wrap">
            <img v-if="server.imageUrl" :src="server.imageUrl" :alt="server.name" class="rental-img" loading="lazy" />
            <div v-else class="rental-placeholder">
              <CloudOutlined class="placeholder-icon" />
              <span>CortexNEL 租赁服</span>
            </div>

            <!-- 顶部浮动徽标 -->
            <div class="cover-top-bar">
              <a-tag v-if="server.hasPassword" color="warning" class="badge-pwd">
                <template #icon><LockOutlined /></template>
                需密码
              </a-tag>
              <div v-else class="badge-public">公开直连</div>

              <div class="badge-player-chip">
                <UserOutlined />
                <span>{{ server.playerCount }}/{{ server.capacity || '?' }}</span>
              </div>
            </div>
          </div>

          <div class="rental-body">
            <div class="rental-title-line">
              <h3 class="rental-name" :title="server.name || server.serverName">{{ server.name || server.serverName || '未命名租赁服' }}</h3>
            </div>

            <div class="rental-meta-line">
              <code class="rental-id">#{{ server.entityId }}</code>
              <a-tag color="processing" class="rental-version-tag">{{ server.mcVersion || '版本未知' }}</a-tag>
            </div>

            <p class="rental-brief" :title="cleanSummary(server.briefSummary)">{{ cleanSummary(server.briefSummary) }}</p>
          </div>

          <div class="rental-card-footer">
            <a-button type="primary" size="small" block :disabled="detailLoading" @click="showDetail(server)">
              <template #icon><EyeOutlined /></template>
              查看详情与进入
            </a-button>
          </div>
        </div>
      </div>
    </a-card>

    <!-- 租赁服详情与进入弹窗 -->
    <a-modal
      :open="!!detail"
      :title="detail?.name || detail?.serverName"
      :width="480"
      @cancel="detail = null"
    >
      <div v-if="detail" class="detail-modal-body">
        <img v-if="detail.imageUrl" :src="detail.imageUrl" :alt="detail.name" class="detail-img" />

        <a-descriptions :column="2" size="small" bordered class="mb">
          <a-descriptions-item label="服务器号">
            <code>{{ detail.entityId }}</code>
          </a-descriptions-item>
          <a-descriptions-item label="在线人数">
            {{ detail.playerCount }} / {{ detail.capacity || '?' }}
          </a-descriptions-item>
          <a-descriptions-item label="目标版本">
            {{ detail.mcVersion || '未知' }}
          </a-descriptions-item>
          <a-descriptions-item label="密码保护">
            {{ detail.hasPassword ? '需要密码' : '公开直连' }}
          </a-descriptions-item>
        </a-descriptions>

        <div class="detail-description" v-html="detail.briefSummary || '暂无详细介绍。'"></div>

        <a-alert v-if="roleError" type="warning" show-icon class="mb" :message="roleError" />
        <a-alert v-if="joinError" type="error" show-icon class="mb" :message="joinError" />

        <div class="role-section">
          <label class="form-label">选择进服角色</label>
          <a-select
            v-if="roles.length"
            v-model:value="selectedRole"
            :get-popup-container="popupContainer"
            style="width: 100%"
            :options="roles.map((r) => ({ value: r.name, label: r.name }))"
            placeholder="选择已有角色"
          />
          <p v-else class="role-empty">暂无角色，请在下方创建。</p>
        </div>

        <div class="create-role-section">
          <label class="form-label">新建角色</label>
          <a-space-compact block>
            <a-input v-model:value="newRole" placeholder="新角色名..." @press-enter="createRole" />
            <a-button type="primary" :disabled="detailLoading || !newRole.trim()" @click="createRole">
              <template #icon><PlusOutlined /></template>
              创建
            </a-button>
          </a-space-compact>
        </div>

        <div v-if="detail.hasPassword" class="password-section">
          <label class="form-label">租赁服密码</label>
          <a-input-password v-model:value="password" placeholder="输入该租赁服连接密码" />
        </div>
      </div>

      <template #footer>
        <div class="modal-footer-flex">
          <a-button @click="detail = null">关闭</a-button>
          <a-space :size="8">
            <a-button
              :disabled="!selectedRole"
              :loading="joinBusy && launchMode === 'proxy'"
              @click="handleRentalLaunch('proxy')"
            >
              <template #icon><ApiOutlined /></template>
              仅代理启动
            </a-button>
            <a-button
              type="primary"
              :disabled="!selectedRole"
              :loading="joinBusy && launchMode === 'client'"
              @click="handleRentalLaunch('client')"
            >
              <template #icon><PlayCircleOutlined /></template>
              启动白端进服
            </a-button>
          </a-space>
        </div>
      </template>
    </a-modal>
  </section>
</template>

<style scoped>
.rental-panel {
  box-shadow: var(--n-card-shadow);
}

.center-spin {
  display: grid;
  place-items: center;
  padding: 40px 0;
}

.rental-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(250px, 1fr));
  gap: 14px;
}

.rental-card {
  display: flex;
  flex-direction: column;
  background: var(--n-bg-card);
  border: 1px solid var(--n-separator);
  border-radius: var(--n-radius);
  overflow: hidden;
  transition: transform var(--n-trans), box-shadow var(--n-trans), border-color var(--n-trans);
}

.rental-card:hover {
  transform: translateY(-2px);
  box-shadow: var(--n-card-shadow);
  border-color: rgba(0, 122, 255, 0.35);
}

.rental-cover-wrap {
  position: relative;
  width: 100%;
  height: 105px;
  background: var(--n-terminal-bg);
  overflow: hidden;
}

.rental-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.rental-placeholder {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  background: var(--n-terminal-bg);
  color: var(--n-text-3);
  font-size: 12px;
}

.placeholder-icon {
  font-size: 26px;
  color: var(--n-blue);
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

.badge-pwd {
  margin: 0;
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.3);
}

.badge-public {
  display: inline-flex;
  align-items: center;
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 10px;
  background: var(--n-scrim);
  backdrop-filter: blur(8px);
  color: var(--n-terminal-text);
}

.badge-player-chip {
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

.rental-body {
  padding: 10px 12px;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.rental-title-line {
  display: flex;
  align-items: center;
}

.rental-name {
  font-size: 13.5px;
  font-weight: 600;
  margin: 0;
  color: var(--n-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.rental-meta-line {
  display: flex;
  align-items: center;
  gap: 6px;
}

.rental-id {
  font-family: var(--n-mono);
  font-size: 11px;
  color: var(--n-text-3);
  max-width: 90px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.rental-version-tag {
  font-size: 10.5px;
  padding: 0 4px;
  margin: 0;
}

.rental-brief {
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

.rental-card-footer {
  padding: 8px 12px;
  border-top: 1px solid var(--n-separator);
  background: var(--n-surface-subtle);
}

.detail-img {
  width: 100%;
  max-height: 180px;
  object-fit: cover;
  border-radius: var(--n-radius);
  margin-bottom: 12px;
  border: 1px solid var(--n-separator);
}

.detail-description {
  font-size: 12.5px;
  color: var(--n-text);
  line-height: 1.6;
  margin: 0 0 14px;
  max-height: 160px;
  overflow-y: auto;
  word-break: break-word;
}

.detail-description :deep(p) {
  margin: 0 0 6px;
}

.detail-description :deep(p:last-child) {
  margin-bottom: 0;
}

.detail-description :deep(strong) {
  font-weight: 700;
}

.role-section,
.create-role-section,
.password-section {
  margin-bottom: 12px;
}

.form-label {
  display: block;
  font-size: 12px;
  font-weight: 600;
  color: var(--n-text-2);
  margin-bottom: 6px;
}

.role-empty {
  font-size: 12px;
  color: var(--n-text-3);
  margin: 0;
}

.modal-footer-flex {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}
</style>
