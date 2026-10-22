<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import {
  CheckCircleFilled,
  EditOutlined,
  LogoutOutlined,
  PlusOutlined,
  ReloadOutlined,
  SafetyOutlined,
  TranslationOutlined,
  UserOutlined,
} from '@ant-design/icons-vue'
import AccountAddModal from '../components/AccountAddModal.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { updateAccountLabel, updateAccountRoleName } from '../api/account'
import type { Account } from '../api/account'
import { useAccountStore } from '../stores/accountStore'
import { useSystemStore } from '../stores/systemStore'
import { generateRoleName, convertHomophone } from '../api/system'

const accounts = useAccountStore()
const system = useSystemStore()

// 弹窗显隐控制
const addModalOpen = ref(false)
const editModalOpen = ref(false)
const confirmDeleteOpen = ref(false)

// 编辑账号
const editingAccount = ref<Account | null>(null)
const editLabel = ref('')
const editRole = ref('')
const savingEdit = ref(false)

// 待删除账号
const pendingDeleteAccount = ref<Account | null>(null)

const columns = [
  { key: 'user', title: '用户 / 昵称', minWidth: 220 },
  { key: 'channel', title: '登录渠道', width: 110 },
  { key: 'role', title: '游戏角色名', width: 150 },
  { key: 'cred', title: '凭据存储安全态', width: 160 },
  { key: 'actions', title: '操作', width: 230 },
]

onMounted(async () => {
  await Promise.all([accounts.refresh(), system.refresh()])
  accounts.mainAccountID = system.settings?.mainAccountId ?? ''
  if (!accounts.mainAccountID && accounts.accounts.length) {
    await accounts.switchTo(accounts.accounts[0].id)
  }
})

function openAddModal(): void {
  addModalOpen.value = true
}

const genRoleLoading = ref(false)

async function randomizeEditRole(): Promise<void> {
  genRoleLoading.value = true
  try {
    const name = await generateRoleName({
      prefix: system.settings?.roleNamePrefix || '',
      suffix: system.settings?.roleNameSuffix || '',
      style: system.settings?.roleNameStyle || 'netease',
      baseWord: system.settings?.roleNameBaseWord || '',
      digits: system.settings?.roleNameDigits ?? 0,
      useHomophone: system.settings?.roleNameHomophone ?? false,
      exactTone: system.settings?.roleNameExactTone ?? false,
      allowSelf: system.settings?.roleNameAllowSelf ?? true,
    })
    if (name) {
      editRole.value = name
      message.success('已随机生成角色名: ' + name)
    }
  } catch (e) {
    message.error('随机生成失败: ' + String(e))
  } finally {
    genRoleLoading.value = false
  }
}

const homophoneLoading = ref(false)

async function homophoneEditRole(): Promise<void> {
  const current = editRole.value.trim()
  if (!current) {
    message.warning('请先输入要转换的角色名或先点击随机生成')
    return
  }
  homophoneLoading.value = true
  try {
    const res = await convertHomophone(current, false, true)
    editRole.value = res
    message.success('已转换为谐音: ' + res)
  } catch (e) {
    message.error('谐音转换失败: ' + String(e))
  } finally {
    homophoneLoading.value = false
  }
}

function openEditModal(a: Account): void {
  editingAccount.value = a
  editLabel.value = a.label
  editRole.value = a.roleName
  editModalOpen.value = true
}

async function saveEditModal(): Promise<void> {
  if (!editingAccount.value) return
  savingEdit.value = true
  try {
    const id = editingAccount.value.id
    if (editLabel.value.trim() !== editingAccount.value.label) {
      await updateAccountLabel(id, editLabel.value.trim())
    }
    if (editRole.value.trim() !== editingAccount.value.roleName) {
      await updateAccountRoleName(id, editRole.value.trim())
    }
    await accounts.refresh()
    message.success('账号信息已更新')
    editModalOpen.value = false
  } catch (e) {
    message.error(String(e))
  } finally {
    savingEdit.value = false
  }
}

async function onSwitchAccount(a: Account): Promise<void> {
  try {
    await accounts.switchTo(a.id)
    message.success(`已将「${a.label || a.roleName || a.userId}」设为出战主账号`)
  } catch (e) {
    message.error(String(e))
  }
}

function requestDelete(a: Account): void {
  pendingDeleteAccount.value = a
  confirmDeleteOpen.value = true
}

async function onConfirmDelete(): Promise<void> {
  confirmDeleteOpen.value = false
  if (!pendingDeleteAccount.value) return
  const id = pendingDeleteAccount.value.id
  await accounts.logout(id)
  message.success('账号已成功移除')
  pendingDeleteAccount.value = null
}
</script>

<template>
  <section class="page">
    <header class="page-header">
      <div>
        <h1 class="page-title">账号管理</h1>
        <p class="page-desc">管理网易通行证与 4399 渠道游戏账号，维护游戏角色名</p>
      </div>
      <a-space :size="12">
        <a-tag color="blue">已绑定 {{ accounts.accounts.length }} 个账号</a-tag>
        <a-button type="primary" @click="openAddModal">
          <template #icon><PlusOutlined /></template>
          添加账号
        </a-button>
      </a-space>
    </header>

    <a-alert
      v-if="accounts.error"
      type="error"
      show-icon
      closable
      class="mb"
      :message="accounts.error"
      @close="accounts.error = ''"
    />

    <!-- 已导入账号列表 -->
    <a-card :bordered="false" class="table-card">
      <template #title><span class="card-title">已绑定游戏账号</span></template>
      <template #extra>
        <span class="card-sub">绿色「出战主号」徽章将作为会话默认认证主体</span>
      </template>

      <a-table
        :data-source="accounts.accounts"
        :columns="columns"
        row-key="id"
        :loading="accounts.loading"
        :pagination="false"
        :scroll="{ x: 860 }"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'user'">
            <div class="user-cell">
              <a-avatar
                :style="{
                  background: record.channel.includes('4399')
                    ? 'linear-gradient(135deg, var(--n-orange) 0%, var(--n-red) 100%)'
                    : 'linear-gradient(135deg, var(--n-blue) 0%, var(--n-teal) 100%)',
                  fontWeight: 700,
                }"
              >
                {{ (record.label || record.roleName || 'U').charAt(0).toUpperCase() }}
              </a-avatar>
              <div class="user-meta">
                <div class="user-title">
                  <strong class="user-name">{{ record.label || record.roleName || '未命名账号' }}</strong>
                  <a-tag v-if="accounts.mainAccountID === record.id" color="success" class="tag-main">
                    <template #icon><CheckCircleFilled /></template>
                    出战主号
                  </a-tag>
                </div>
                <span class="user-id">UID: {{ record.userId }}</span>
              </div>
            </div>
          </template>

          <template v-else-if="column.key === 'channel'">
            <a-tag :color="record.channel.includes('4399') ? 'orange' : 'processing'">{{ record.channel }}</a-tag>
          </template>

          <template v-else-if="column.key === 'role'">
            <span :class="{ 'role-muted': !record.roleName }">{{ record.roleName || '未设定角色' }}</span>
          </template>

          <template v-else-if="column.key === 'cred'">
            <a-tooltip
              :title="record.tokenPersisted
                ? 'Windows DPAPI 当前系统用户安全加密落盘，会话过期后可自动重连'
                : '凭据仅保留在当前应用运行内存中，关闭后即销毁'"
            >
              <a-tag :color="record.tokenPersisted ? 'success' : 'default'">
                <template #icon><SafetyOutlined /></template>
                {{ record.tokenPersisted ? 'DPAPI 加密落盘' : '仅会话内存' }}
              </a-tag>
            </a-tooltip>
          </template>

          <template v-else-if="column.key === 'actions'">
            <a-space :size="8">
              <a-button
                size="small"
                :type="accounts.mainAccountID === record.id ? 'primary' : 'default'"
                :ghost="accounts.mainAccountID === record.id"
                :disabled="accounts.mainAccountID === record.id"
                @click="onSwitchAccount(record)"
              >
                {{ accounts.mainAccountID === record.id ? '出战中' : '使用账号' }}
              </a-button>
              <a-button size="small" @click="openEditModal(record)">
                <template #icon><EditOutlined /></template>
                编辑
              </a-button>
              <a-button size="small" danger @click="requestDelete(record)">
                <template #icon><LogoutOutlined /></template>
                删除
              </a-button>
            </a-space>
          </template>
        </template>

        <template #emptyText>
          <div class="account-empty">
            <UserOutlined class="empty-icon" />
            <p>暂无绑定的游戏账号</p>
            <span>点击右上角「添加账号」登录 4399 账号或导入 sauth Cookie</span>
            <div style="margin-top: 14px">
              <a-button type="primary" @click="openAddModal">
                <template #icon><PlusOutlined /></template>
                立即添加账号
              </a-button>
            </div>
          </div>
        </template>
      </a-table>
    </a-card>

    <AccountAddModal v-model:open="addModalOpen" />

    <!-- 弹窗 2：编辑账号信息 -->
    <a-modal
      v-model:open="editModalOpen"
      title="编辑账号信息"
      :confirm-loading="savingEdit"
      ok-text="保存修改"
      cancel-text="取消"
      :width="460"
      @ok="saveEditModal"
    >
      <div class="form-col edit-form-col">
        <div class="account-brief-row" v-if="editingAccount">
          <a-avatar
            :style="{
              background: editingAccount.channel.includes('4399')
                ? 'linear-gradient(135deg, var(--n-orange) 0%, var(--n-red) 100%)'
                : 'linear-gradient(135deg, var(--n-blue) 0%, var(--n-teal) 100%)',
              fontWeight: 700,
            }"
          >
            {{ (editingAccount.label || editingAccount.roleName || 'U').charAt(0).toUpperCase() }}
          </a-avatar>
          <div>
            <div style="font-weight: 600">{{ editingAccount.label || editingAccount.roleName || '未命名账号' }}</div>
            <span style="font-size: 11px; color: var(--n-text-2); font-family: var(--n-mono)">UID: {{ editingAccount.userId }} · {{ editingAccount.channel }}</span>
          </div>
        </div>

        <label class="form-label">自定义账号备注</label>
        <a-input
          v-model:value="editLabel"
          placeholder="例如：主玩大号 / 备用小号"
          allow-clear
        />

        <div style="display: flex; justify-content: space-between; align-items: center; margin-top: 10px; margin-bottom: 4px;">
          <label class="form-label" style="margin-bottom: 0;">游戏内角色名 (In-game Role Name)</label>
          <a-space :size="8">
            <a-button type="link" size="small" :loading="homophoneLoading" @click="homophoneEditRole" style="padding: 0;">
              <template #icon><TranslationOutlined /></template>
              谐音转换
            </a-button>
            <a-button type="link" size="small" :loading="genRoleLoading" @click="randomizeEditRole" style="padding: 0;">
              <template #icon><ReloadOutlined /></template>
              随机生成
            </a-button>
          </a-space>
        </div>
        <a-input
          v-model:value="editRole"
          placeholder="输入进服使用的角色名（如：Steve）"
          allow-clear
        />
      </div>
    </a-modal>

    <!-- 弹窗 3：退出/删除账号确认 -->
    <ConfirmDialog
      :open="confirmDeleteOpen"
      title="移除账号确认"
      :message="`确认要移除账号「${pendingDeleteAccount?.label || pendingDeleteAccount?.roleName || pendingDeleteAccount?.userId}」吗？本地存储的加密凭据将被完全销毁。`"
      ok-text="确认移除"
      @confirm="onConfirmDelete"
      @cancel="confirmDeleteOpen = false"
    />
  </section>
</template>

<style scoped>
.table-card {
  box-shadow: var(--n-card-shadow);
  overflow: hidden;
}

:deep(.ant-table-cell) {
  white-space: nowrap;
}

.user-cell {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.user-meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.user-title {
  display: flex;
  align-items: center;
  gap: 6px;
}

.user-name {
  font-size: 13.5px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tag-main {
  margin-inline-end: 0;
}

.user-id {
  font-size: 11px;
  color: var(--n-text-2);
  font-family: var(--n-mono);
}

.role-muted {
  color: var(--n-text-3);
  font-style: italic;
}

.account-empty {
  padding: 36px 0 24px;
  color: var(--n-text-2);
}

.empty-icon {
  font-size: 32px;
  color: var(--n-text-3);
  margin-bottom: 8px;
}

.account-empty p {
  margin: 6px 0 2px;
  font-weight: 600;
  font-size: 13.5px;
}

.account-empty span {
  font-size: 12px;
  color: var(--n-text-3);
}

.form-col {
  display: flex;
  flex-direction: column;
  gap: 12px;
  max-width: 480px;
}

.form-label {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--n-text-2);
  margin-bottom: -6px;
}
</style>
