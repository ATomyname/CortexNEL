<!-- 进服角色弹窗：从 ServersView.vue 拆出（规范 §4 文件职责拆分）。
     自管家"新建角色名"输入与随机/谐音生成；角色列表与创建动作由父视图经 props/事件驱动
     （创建需要服务器 entityId，属于父视图的领域数据）。 -->
<script setup lang="ts">
import { ref } from 'vue'
import { message } from 'ant-design-vue'
import {
  ApiOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  TranslationOutlined,
} from '@ant-design/icons-vue'
import type { Role } from '../api/network'
import { generateRoleName, convertHomophone } from '../api/system'
import { useSystemStore } from '../stores/systemStore'
import { popupContainer } from '../lib/popupContainer'

const system = useSystemStore()

defineProps<{
  open: boolean
  serverName: string
  roles: Role[]
  selectedRole: string
  roleError: string
  loading: boolean
  busy: boolean
  launchMode: 'proxy' | 'client'
  /** 白端会话是否在跑（一键进服单开位被占）。仅代理多开不受此限制。 */
  mirrorRunning: boolean
}>()

const emit = defineEmits<{
  (e: 'cancel'): void
  (e: 'update:selectedRole', value: string): void
  (e: 'update:roleError', value: string): void
  (e: 'create', name: string): void
  (e: 'launch', mode: 'proxy' | 'client'): void
}>()

const newRole = ref('')
const randomRoleLoading = ref(false)
const homophoneNewRoleLoading = ref(false)

async function randomizeNewRole(): Promise<void> {
  randomRoleLoading.value = true
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
      newRole.value = name
    }
  } catch (e) {
    message.error('随机角色名生成失败: ' + String(e))
  } finally {
    randomRoleLoading.value = false
  }
}

async function homophoneNewRole(): Promise<void> {
  const current = newRole.value.trim()
  if (!current) {
    message.warning('请先输入要转换的角色名或先点击随机生成')
    return
  }
  homophoneNewRoleLoading.value = true
  try {
    const res = await convertHomophone(current, false, true)
    newRole.value = res
    message.success('已转换为谐音: ' + res)
  } catch (e) {
    message.error('谐音转换失败: ' + String(e))
  } finally {
    homophoneNewRoleLoading.value = false
  }
}

function onCreate(): void {
  emit('create', newRole.value.trim())
}
</script>

<template>
  <a-modal :open="open" title="选择进服角色" :width="480" @cancel="emit('cancel')">
    <p class="dialog-sub">目标服务器：<strong>{{ serverName }}</strong></p>
    <a-alert
      v-if="roleError"
      type="warning"
      show-icon
      closable
      class="mb"
      :message="roleError"
      @close="emit('update:roleError', '')"
    />

    <div class="role-form">
      <label class="form-label">选择已有角色</label>
      <a-select
        v-if="roles.length"
        :value="selectedRole"
        :get-popup-container="popupContainer"
        style="width: 100%"
        :options="roles.map((r) => ({ value: r.name, label: r.name }))"
        placeholder="选择角色"
        @update:value="emit('update:selectedRole', $event)"
      />
      <p v-else class="role-empty">该账号在此服务器下暂无角色，请在下方新建角色后进入。</p>
    </div>

    <div class="create-role">
      <p class="role-tip">角色名就是你在该服务器里显示的名字，可随机生成，也可以自己填一个。</p>
      <div class="create-role-head">
        <label class="form-label">或新建角色</label>
        <a-space :size="8">
          <a-button type="link" size="small" :loading="homophoneNewRoleLoading" @click="homophoneNewRole" style="padding: 0;">
            <template #icon><TranslationOutlined /></template>
            谐音转换
          </a-button>
          <a-button type="link" size="small" :loading="randomRoleLoading" @click="randomizeNewRole" style="padding: 0;">
            <template #icon><ReloadOutlined /></template>
            随机生成
          </a-button>
        </a-space>
      </div>
      <a-space-compact block>
        <a-input
          v-model:value="newRole"
          placeholder="输入新角色名..."
          :disabled="loading"
          @press-enter="onCreate"
        />
        <a-button type="primary" :loading="loading" :disabled="!newRole.trim()" @click="onCreate">
          <template #icon><PlusOutlined /></template>
          创建
        </a-button>
      </a-space-compact>
    </div>

    <template #footer>
      <div class="modal-footer-flex">
        <a-button @click="emit('cancel')">取消</a-button>
        <a-space :size="8">
          <a-button
            :disabled="!selectedRole || loading"
            :loading="busy && launchMode === 'proxy'"
            @click="emit('launch', 'proxy')"
          >
            <template #icon><ApiOutlined /></template>
            仅代理启动
          </a-button>
          <a-tooltip :title="mirrorRunning ? '已有一个正在自动进服的会话，请先停止它' : ''">
            <a-button
              type="primary"
              :disabled="!selectedRole || loading || mirrorRunning"
              :loading="busy && launchMode === 'client'"
              @click="emit('launch', 'client')"
            >
              <template #icon><PlayCircleOutlined /></template>
              启动白端进服
            </a-button>
          </a-tooltip>
        </a-space>
      </div>
    </template>
  </a-modal>
</template>

<style scoped>
.dialog-sub {
  font-size: 13px;
  color: var(--n-text-2);
  margin: 0 0 12px;
}

.role-form {
  margin-bottom: 14px;
}

.form-label {
  display: block;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--n-text-2);
  margin-bottom: 6px;
}

.role-empty {
  font-size: 12.5px;
  color: var(--n-text-3);
  margin: 0;
}

.create-role {
  padding-top: 12px;
  border-top: 1px solid var(--n-separator);
}

.create-role-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 4px;
}

.role-tip {
  font-size: 12px;
  color: var(--n-text-2);
  margin: 0 0 8px;
}

.modal-footer-flex {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}
</style>
