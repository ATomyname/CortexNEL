<!-- 代理管理页：多代理的启动/停止/总览。
     与概览页的分界：概览是"主组合一键进服"的快捷入口，本页管"所有代理"的状态。 -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import {
  PlusOutlined,
  StopOutlined,
} from '@ant-design/icons-vue'
import StatusBadge from '../components/StatusBadge.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { useSessionStore, type ProxySession } from '../stores/sessionStore'
import { useAccountStore } from '../stores/accountStore'
import { useProfileStore } from '../stores/profileStore'
import { popupContainer } from '../lib/popupContainer'

const session = useSessionStore()
const accounts = useAccountStore()
const profiles = useProfileStore()

const busy = ref(false)
const confirmStopAll = ref(false)
const confirmStopOne = ref<string>('')

// 新增代理表单
const addOpen = ref(false)
const addServerId = ref('')
const addAccountId = ref('')
const addMirror = ref(false)

const serverOptions = computed(() =>
  profiles.servers.map((s) => ({ value: s.id, label: `${s.name}（${s.mcVersion || '1.20'}）` })),
)
const accountOptions = computed(() =>
  accounts.accounts.map((a) => ({ value: a.id, label: a.label || a.roleName || a.id })),
)

/** 选中账号的角色名（后端按 (服务器, 角色) 判重，要同源取值）。 */
const selectedRoleName = computed(
  () => accounts.accounts.find((a) => a.id === addAccountId.value)?.roleName ?? '',
)
/** 已占用该 (服务器, 角色) 的会话；非空即命中后端唯一约束。 */
const duplicateSession = computed(() =>
  session.sessions.find(
    (s) => s.serverId === addServerId.value && (s.roleName ?? '') === selectedRoleName.value,
  ) ?? null,
)

onMounted(async () => {
  await Promise.all([session.refresh(), accounts.refresh(), profiles.refresh()])
  if (!addAccountId.value) addAccountId.value = accounts.accounts[0]?.id ?? ''
  if (!addServerId.value) addServerId.value = profiles.servers[0]?.id ?? ''
})

// 阶段可能长时间停留（等客户端接入），定时轮询兜底，避免事件漏推时界面发酸。
let poller: number | undefined
onMounted(() => {
  poller = window.setInterval(() => void session.refresh(), 5000)
})
onUnmounted(() => {
  if (poller !== undefined) window.clearInterval(poller)
})

function stageOf(s: ProxySession): string {
  return s.stage || 'IDLE'
}

/** 已运行时长（分钟级，不足 1 分钟显示"<1 分钟"）。 */
function uptimeOf(s: ProxySession): string {
  const started = new Date(s.startedAt).getTime()
  if (!Number.isFinite(started)) return '—'
  const mins = Math.floor((Date.now() - started) / 60000)
  if (mins < 1) return '<1 分钟'
  if (mins < 60) return `${mins} 分钟`
  return `${Math.floor(mins / 60)} 小时 ${mins % 60} 分`
}

function openAdd(): void {
  if (!accounts.accounts.length) {
    message.warning('请先在账号管理登录账号')
    return
  }
  if (!profiles.servers.length) {
    message.warning('请先在服务器页选择或添加目标服务器')
    return
  }
  addOpen.value = true
}

async function submitAdd(): Promise<void> {
  if (!addServerId.value || !addAccountId.value) {
    message.warning('请选择目标服务器与账号')
    return
  }
  if (duplicateSession.value) {
    message.warning(`该服务器上已有相同角色的代理在运行（${duplicateSession.value.sessionId}），请先停止它`)
    return
  }
  busy.value = true
  try {
    const id = await session.startWithTarget(addServerId.value, addAccountId.value, addMirror.value)
    message.success(`已启动代理 ${id}`)
    addOpen.value = false
  } catch (e) {
    message.error(String(e))
  } finally {
    busy.value = false
  }
}

function askStopOne(id: string): void {
  confirmStopOne.value = id
}

async function doStopOne(): Promise<void> {
  const id = confirmStopOne.value
  confirmStopOne.value = ''
  if (!id) return
  try {
    await session.stop(id)
    message.success('已停止')
  } catch (e) {
    message.error(String(e))
  }
}

async function doStopAll(): Promise<void> {
  confirmStopAll.value = false
  try {
    await session.stopAll()
    message.success('已停止全部代理')
  } catch (e) {
    message.error(String(e))
  }
}
</script>

<template>
  <section class="page">
    <header class="page-header">
      <div>
        <h1 class="page-title">代理管理</h1>
        <p class="page-desc">管理全部运行中的本地中继代理；仅代理可任意多开，一键进服同时只允许一个</p>
      </div>
      <a-space :size="8" wrap>
        <a-button type="primary" :loading="busy" @click="openAdd">
          <template #icon><PlusOutlined /></template>
          新增代理
        </a-button>
      </a-space>
    </header>

    <a-alert
      v-if="session.error"
      type="error"
      show-icon
      closable
      class="mb"
      :message="session.error"
      @close="session.error = ''"
    />

    <!-- 空态 -->
    <a-card v-if="session.sessions.length === 0" :bordered="false" class="main-card">
      <a-empty description="当前没有运行中的代理">
        <a-button type="primary" @click="openAdd">新增代理</a-button>
      </a-empty>
    </a-card>

    <!-- 代理列表 -->
    <template v-else>
      <a-card :bordered="false" class="main-card mb">
        <template #title><span class="card-title">运行中的代理（{{ session.sessions.length }}）</span></template>
        <template #extra>
          <a-button danger size="small" :disabled="busy" @click="confirmStopAll = true">
            <template #icon><StopOutlined /></template>
            全部停止
          </a-button>
        </template>

        <a-list item-layout="horizontal" :data-source="session.sessions" :split="true">
          <template #renderItem="{ item }">
            <a-list-item>
              <a-list-item-meta>
                <template #title>
                  <a-space :size="8" wrap>
                    <span class="sess-id">{{ item.sessionId }}</span>
                    <a-tag v-if="item.launchMirror" color="purple">白端</a-tag>
                    <a-tag v-else color="blue">仅代理</a-tag>
                    <StatusBadge :stage="stageOf(item)" :active="true" size="sm" />
                  </a-space>
                </template>
                <template #description>
                  <div class="sess-meta">
                    <span>服务器：{{ item.serverId }}</span>
                    <span v-if="item.roleName">角色：{{ item.roleName }}</span>
                    <span>地址：<code class="addr">{{ item.listenAddr }}</code></span>
                    <span>已运行：{{ uptimeOf(item) }}</span>
                  </div>
                </template>
              </a-list-item-meta>
              <template #actions>
                <a-button size="small" danger :disabled="busy" @click="askStopOne(item.sessionId)">
                  <template #icon><StopOutlined /></template>
                  停止
                </a-button>
              </template>
            </a-list-item>
          </template>
        </a-list>
      </a-card>
    </template>

    <!-- 新增代理弹窗 -->
    <a-modal
      :open="addOpen"
      title="新增代理"
      :width="480"
      :confirm-loading="busy"
      @ok="submitAdd"
      @cancel="addOpen = false"
    >
      <a-form layout="vertical">
        <a-form-item label="目标服务器">
          <a-select
            v-model:value="addServerId"
            :options="serverOptions"
            :get-popup-container="popupContainer"
            placeholder="选择服务器"
          />
        </a-form-item>
        <a-form-item label="使用账号">
          <a-select
            v-model:value="addAccountId"
            :options="accountOptions"
            :get-popup-container="popupContainer"
            placeholder="选择账号"
          />
        </a-form-item>
        <a-form-item label="启动方式">
          <a-radio-group v-model:value="addMirror">
            <a-radio :value="false">仅代理（监听端口，客户端手动连入；可多开）</a-radio>
            <a-radio :value="true">一键进服（自动拉起白端；同时只允许一个）</a-radio>
          </a-radio-group>
        </a-form-item>
        <a-alert
          v-if="duplicateSession"
          type="warning"
          show-icon
          class="mb"
          :message="`该服务器上已有相同角色的代理在运行（${duplicateSession.sessionId}）`"
          description="同一服务器 + 同一角色不允许同时起多个代理，请先停止它或改用其他账号。"
        />
      </a-form>
    </a-modal>

    <ConfirmDialog
      :open="confirmStopAll"
      title="停止全部代理"
      message="将停止所有运行中的代理会话（含白端）。确定继续？"
      @confirm="doStopAll"
      @cancel="confirmStopAll = false"
    />

    <ConfirmDialog
      :open="!!confirmStopOne"
      title="停止该代理"
      :message="`确定停止 ${confirmStopOne} 吗？其他代理不受影响。`"
      @confirm="doStopOne"
      @cancel="confirmStopOne = ''"
    />
  </section>
</template>

<style scoped>
.main-card {
  box-shadow: var(--n-card-shadow);
}

.card-title {
  font-weight: 600;
}

.sess-id {
  font-family: var(--n-mono);
  font-size: 12.5px;
  color: var(--n-text-2);
}

.sess-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 16px;
  font-size: 12.5px;
  color: var(--n-text-2);
}

.addr {
  font-family: var(--n-mono);
  font-size: 12px;
  color: var(--n-text);
}
</style>
