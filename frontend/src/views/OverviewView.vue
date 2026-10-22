<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  ClusterOutlined,
  PlayCircleOutlined,
  PlusCircleOutlined,
  UserOutlined,
} from '@ant-design/icons-vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import StatusBadge from '../components/StatusBadge.vue'
import { message } from 'ant-design-vue'
import { useAccountStore } from '../stores/accountStore'
import { useLogStore } from '../stores/logStore'
import { useProfileStore } from '../stores/profileStore'
import { useSessionStore } from '../stores/sessionStore'
import { useSystemStore } from '../stores/systemStore'
import { popupContainer } from '../lib/popupContainer'

const router = useRouter()
const session = useSessionStore()
const accounts = useAccountStore()
const profiles = useProfileStore()
const system = useSystemStore()
const logs = useLogStore()

const confirmOpen = ref(false)
const confirmProxyOnlyOpen = ref(false)
const starting = ref(false)

onMounted(async () => {
  if (!(window as Window & { go?: unknown }).go) {
    session.error = '请在 CortexNEL 桌面应用容器中运行，当前为预览环境。'
    return
  }
  await Promise.all([
    session.refresh(),
    accounts.refresh(),
    profiles.refresh(),
    system.refresh(),
  ])
  if (!system.settings?.mainAccountId && accounts.accounts.length) {
    await accounts.switchTo(accounts.accounts[0].id)
  }
})

const currentAccount = computed(() =>
  accounts.accounts.find((a) => a.id === system.settings?.mainAccountId),
)
const currentServer = computed(() =>
  profiles.servers.find((s) => s.id === system.settings?.mainServerId),
)

async function onQuickSwitchServer(serverId: string): Promise<void> {
  if (!system.settings) return
  try {
    await system.saveSettings({ ...system.settings, mainServerId: serverId })
    message.success('已切换目标服务器')
  } catch (e) {
    message.error('切换服务器失败: ' + String(e))
  }
}

// 协议版本映射与动态自适应感知
function getProtocolInfo(mcVersion?: string): { protocol: number | string; versionStr: string; tag: string } {
  if (!mcVersion) {
    return { protocol: 763, versionStr: '1.20.1', tag: '默认自适应' }
  }
  const v = mcVersion.trim()
  if (v.startsWith('1.21.4')) return { protocol: 769, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.21.2') || v.startsWith('1.21.3')) return { protocol: 768, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.21')) return { protocol: 767, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.20.5') || v.startsWith('1.20.6')) return { protocol: 766, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.20.3') || v.startsWith('1.20.4')) return { protocol: 765, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.20.2')) return { protocol: 764, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.20')) return { protocol: 763, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.19.4')) return { protocol: 762, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.19.3')) return { protocol: 761, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.19.1') || v.startsWith('1.19.2')) return { protocol: 760, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.19')) return { protocol: 759, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.18.2')) return { protocol: 758, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.18')) return { protocol: 757, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.17.1')) return { protocol: 756, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.17')) return { protocol: 755, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.16.4') || v.startsWith('1.16.5')) return { protocol: 754, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.16.2') || v.startsWith('1.16.3')) return { protocol: 751, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.16.1')) return { protocol: 736, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.16')) return { protocol: 735, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.15.2')) return { protocol: 578, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.14.4')) return { protocol: 498, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.13.2')) return { protocol: 404, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.12')) return { protocol: 340, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.11')) return { protocol: 316, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.10')) return { protocol: 210, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.9.4')) return { protocol: 110, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.8')) return { protocol: 47, versionStr: v, tag: '自动适配' }
  if (v.startsWith('1.7')) return { protocol: 5, versionStr: v, tag: '自动适配' }
  return { protocol: '自适应', versionStr: v, tag: '动态感知' }
}

const currentProtocol = computed(() => getProtocolInfo(currentServer.value?.mcVersion))

/** 客户端未连入提示：已等待分钟数（取整，至少 1）。 */
const attachWaitedMinutes = computed(() => {
  const ms = session.attachWaiting?.waitedMs ?? 0
  return Math.max(1, Math.round(ms / 60000))
})
/** 未连入提示的排查引导：镜像模式与手动模式给不同的下一步。 */
const attachWaitingHint = computed(() =>
  session.attachWaiting?.launchMirror
    ? '若客户端窗口没有出现或已退出，可能是启动被安全软件拦截或 Java 环境异常；可停止该代理后重试，或改用「仅启动代理」手动连接。'
    : '请在 Minecraft 客户端中手动连接上面的本地代理地址；若尚未打开客户端，请先启动。',
)
/** 概览展示的"当前代理"：最近启动的那个，没有则取第一个。 */
const currentProxy = computed(() => session.current)

// 最近 6 条日志（倒序展示最新在前）
const recentLogs = computed(() => logs.lines.slice(-6).reverse())

// 检查配置状态
const missingSteps = computed<{ title: string; desc: string; to: string }[]>(() => {
  const list: { title: string; desc: string; to: string }[] = []
  if (!accounts.accounts.length) {
    list.push({ title: '添加游戏账号', desc: '需要至少登录一个 4399 或网易游戏账号', to: '/accounts' })
  } else if (!system.settings?.mainAccountId) {
    list.push({ title: '选择出战主账号', desc: '在账号页挑选或切换当前出战主账号', to: '/accounts' })
  }

  if (!profiles.servers.length) {
    list.push({ title: '添加目标服务器', desc: '浏览网络服务器列表并挑选或进入服务器', to: '/servers' })
  } else if (!system.settings?.mainServerId) {
    list.push({ title: '选择目标服务器', desc: '在服务器页选择要进入的目标服务器', to: '/servers' })
  }

  return list
})

function requestJoin(): void {
  session.error = ''
  if (!accounts.accounts.length) {
    session.error = '请先添加并登录游戏账号'
    return
  }
  if (!system.settings?.mainAccountId) {
    session.error = '请先选择主账号'
    return
  }
  if (!profiles.servers.length) {
    session.error = '请先添加或选择目标服务器'
    return
  }
  if (!system.settings?.mainServerId) {
    session.error = '请先选择主服务器'
    return
  }
  confirmOpen.value = true
}

function requestProxyOnly(): void {
  session.error = ''
  if (!accounts.accounts.length) {
    session.error = '请先添加并登录游戏账号'
    return
  }
  if (!system.settings?.mainAccountId) {
    session.error = '请先在账号管理中选择主账号'
    return
  }
  if (!profiles.servers.length) {
    session.error = '请先在网络服务器中选择或添加目标服务器'
    return
  }
  if (!system.settings?.mainServerId) {
    session.error = '请先在设置或网络服务器中设为主服务器'
    return
  }
  confirmProxyOnlyOpen.value = true
}

async function onConfirmJoin(): Promise<void> {
  confirmOpen.value = false
  starting.value = true
  try {
    await session.start()
  } catch {
    // 错误已由 store 暴露
  } finally {
    starting.value = false
  }
}

async function onConfirmProxyOnly(): Promise<void> {
  confirmProxyOnlyOpen.value = false
  starting.value = true
  try {
    await session.startProxyOnly()
  } catch {
    // 错误已由 store 暴露
  } finally {
    starting.value = false
  }
}

</script>

<template>
  <section class="page">
    <header class="page-header">
      <div>
        <h1 class="page-title">控制台概览</h1>
        <p class="page-desc">CortexNEL 本地代理与客户端联动中心，启动中继代理并监控连接流转</p>
      </div>
      <StatusBadge
        v-if="currentProxy"
        :stage="currentProxy.stage || 'IDLE'"
        :active="true"
        size="md"
      />
      <StatusBadge v-else stage="IDLE" :active="false" size="md" />
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

    <!-- 进服前置资产补齐进度已提升为全局横幅（components/InstallProgressBanner.vue），
         任何页面触发下载都直接可见，此处不再重复渲染。 -->

    <!-- 快速指引 (未完全配置时展示) -->
    <a-card v-if="missingSteps.length" :bordered="false" class="mb guide-card">
      <template #title><span class="card-title">快速配置指引</span></template>
      <template #extra><a-tag color="blue">完成以下步骤即可一键进服</a-tag></template>
      <a-steps direction="vertical" size="small" :current="0" progress-dot>
        <a-step v-for="(step, idx) in missingSteps" :key="idx" :title="step.title">
          <template #description>
            <div class="guide-desc">
              <span>{{ step.desc }}</span>
              <router-link :to="step.to" class="guide-link">前往配置 →</router-link>
            </div>
          </template>
        </a-step>
      </a-steps>
    </a-card>

    <!-- 4 项核心状态指标 -->
    <a-row :gutter="[14, 14]" class="mb metric-row">
      <a-col :xs="24" :sm="12" :lg="6">
        <a-card :bordered="false" size="small" class="metric-card">
          <a-statistic title="上游延迟" :value="42" suffix="ms" :value-style="{ fontSize: '20px', fontWeight: '700' }" />
          <div class="metric-tag-wrap">
            <a-tooltip title="示例快照：后端延迟遥测绑定尚未接入">
              <a-tag class="metric-tag">示例快照</a-tag>
            </a-tooltip>
          </div>
        </a-card>
      </a-col>
      <a-col :xs="24" :sm="12" :lg="6">
        <a-card :bordered="false" size="small" class="metric-card">
          <a-statistic
            title="本地监听端口"
            :value="currentProxy?.listenAddr || '127.0.0.1:25565'"
            :value-style="{ fontSize: '14.5px', fontFamily: 'var(--n-mono)', fontWeight: '700' }"
          />
          <div class="metric-tag-wrap">
            <a-tag v-if="!currentProxy" class="metric-tag">默认 · 未开启</a-tag>
            <template v-else>
              <a-tag color="success" class="metric-tag">代理监听中</a-tag>
              <a-tag v-if="system.settings?.allowLan" color="blue" class="metric-tag">局域网已开放</a-tag>
            </template>
          </div>
        </a-card>
      </a-col>
      <a-col :xs="24" :sm="12" :lg="6">
        <a-card :bordered="false" size="small" class="metric-card">
          <a-statistic
            title="协议版本"
            :value="currentProtocol.protocol"
            :suffix="`(${currentProtocol.versionStr})`"
            :value-style="{ fontSize: '20px', fontWeight: '700' }"
          />
          <div class="metric-tag-wrap">
            <a-tag :color="currentServer?.mcVersion ? 'success' : 'processing'" class="metric-tag">
              {{ currentProtocol.tag }}
            </a-tag>
          </div>
        </a-card>
      </a-col>
      <a-col :xs="24" :sm="12" :lg="6">
        <a-card :bordered="false" size="small" class="metric-card">
          <a-statistic title="已绑定账号" :value="accounts.accounts.length" suffix="个" :value-style="{ fontSize: '20px', fontWeight: '700' }" />
          <div class="metric-tag-wrap">
            <a-tag color="purple" class="metric-tag">出战准备完毕</a-tag>
          </div>
        </a-card>
      </a-col>
    </a-row>

    <!-- 代理总览：多代理下概览只做汇总，逐代理管理在「代理管理」页 -->
    <div class="hero-grid mb">
      <a-card :bordered="false" title="代理总览" class="hero-card">
        <template #extra>
          <a-tag :color="session.sessions.length ? 'processing' : 'default'">
            {{ session.sessions.length ? `${session.sessions.length} 个代理运行中` : '无运行中的代理' }}
          </a-tag>
        </template>

        <div class="hero-status">
          <StatusBadge
            v-for="s in session.sessions"
            :key="s.sessionId"
            :stage="s.stage || 'IDLE'"
            :active="true"
            size="sm"
          />
          <span v-if="!session.sessions.length" class="listen-empty">未开启代理监听（点击一键进服或仅启动代理后开启）</span>
        </div>

        <!-- 客户端长时间未连入：只读提示 + 排查引导（后端 session.attach.waiting，不迁移状态） -->
        <a-alert
          v-if="session.attachWaiting"
          type="warning"
          show-icon
          class="mb attach-waiting-alert"
        >
          <template #message>
            <span>客户端已等待 {{ attachWaitedMinutes }} 分钟仍未连入</span>
          </template>
          <template #description>
            <span>{{ attachWaitingHint }}</span>
          </template>
        </a-alert>

        <div class="listen-box">
          <span class="listen-label">最近代理地址</span>
          <a-typography-text
            v-if="currentProxy?.listenAddr"
            code
            class="listen-addr"
            :copyable="{ text: currentProxy.listenAddr, tooltip: '复制地址' }"
          >
            {{ currentProxy.listenAddr }}
          </a-typography-text>
          <span v-else class="listen-empty">—</span>
          <a-button v-if="session.sessions.length" type="link" size="small" @click="router.push('/proxies')">
            前往代理管理 →
          </a-button>
        </div>

        <div class="hero-actions">
          <a-button type="primary" size="large" block :loading="starting" @click="requestJoin">
            <template #icon><PlayCircleOutlined /></template>
            一键进服
          </a-button>
          <a-tooltip title="仅开启本地中继代理并监听端口，等待外部或现有客户端手动直连；可多开">
            <a-button size="large" block :disabled="starting" @click="requestProxyOnly">
              <template #icon><PlusCircleOutlined /></template>
              仅启动代理
            </a-button>
          </a-tooltip>
        </div>
      </a-card>

      <!-- 当前出战主组合 -->
      <a-card :bordered="false" title="当前主档案组合" class="hero-card">
        <template #extra>
          <a-button type="link" size="small" @click="router.push('/settings')">管理配置</a-button>
        </template>

        <div class="matrix">
          <div class="matrix-item">
            <span class="item-icon icon-user"><UserOutlined /></span>
            <div class="item-details">
              <span class="item-label">出战账号</span>
              <div v-if="currentAccount" class="item-val">
                <strong>{{ currentAccount.label || currentAccount.roleName || currentAccount.userId }}</strong>
                <a-tag size="small">{{ currentAccount.channel }}</a-tag>
              </div>
              <span v-else class="item-empty">未选择主账号</span>
            </div>
            <a-button type="link" size="small" @click="router.push('/accounts')">切换</a-button>
          </div>

          <div class="matrix-item">
            <span class="item-icon icon-server"><ClusterOutlined /></span>
            <div class="item-details">
              <span class="item-label">目标服务器</span>
              <div v-if="currentServer" class="item-val">
                <strong :title="currentServer.name">{{ currentServer.name }}</strong>
                <code class="item-mono" :title="`${currentServer.host}:${currentServer.port}`">{{ currentServer.host }}:{{ currentServer.port }}</code>
              </div>
              <span v-else class="item-empty">未选择主服务器</span>
            </div>
            <a-space :size="4">
              <a-select
                v-if="profiles.servers.length > 1"
                :value="system.settings?.mainServerId"
                :get-popup-container="popupContainer"
                size="small"
                style="min-width: 130px; max-width: 170px"
                placeholder="快速切换"
                @change="onQuickSwitchServer($event as string)"
              >
                <a-select-option v-for="s in profiles.servers" :key="s.id" :value="s.id">
                  {{ s.name }} ({{ s.mcVersion || '1.20' }})
                </a-select-option>
              </a-select>
              <a-button type="link" size="small" @click="router.push('/servers')">浏览全部</a-button>
            </a-space>
          </div>
        </div>
      </a-card>
    </div>

    <!-- 最近 6 条实时日志监控面板 -->
    <a-card :bordered="false" title="实时日志监控">
      <template #extra>
        <a-space>
          <a-tag color="processing">实时事件流</a-tag>
        </a-space>
      </template>

      <div class="terminal-panel log-terminal">
        <div v-if="!recentLogs.length" class="terminal-empty">
          <span class="terminal-prompt">&gt;</span>
          <span>暂无活动日志。点击「一键进服」或「仅启动代理」后将在此捕获连接事件与协议握手流。</span>
        </div>
        <div v-else>
          <div v-for="(line, idx) in recentLogs" :key="idx" class="log-line">
            <span class="log-ts">{{ line.ts }}</span>
            <span class="log-mod">[{{ line.module }}]</span>
            <span class="log-msg" :class="[`lvl-${line.level}`]">{{ line.message }}</span>
          </div>
        </div>
      </div>
    </a-card>

    <!-- 确认进服弹窗 -->
    <ConfirmDialog
      :open="confirmOpen"
      title="确认一键进服"
      message="将启动本地转发代理，并使用主环境配置直接唤起 Minecraft 客户端连接本地端口。是否继续？"
      @confirm="onConfirmJoin"
      @cancel="confirmOpen = false"
    />

    <!-- 确认仅启动代理弹窗 -->
    <ConfirmDialog
      :open="confirmProxyOnlyOpen"
      title="启动本地代理"
      message="将仅启动本地中继代理服务，监听本地端口并等待外部客户端接入，不会自动拉起游戏。是否继续？"
      @confirm="onConfirmProxyOnly"
      @cancel="confirmProxyOnlyOpen = false"
    />
  </section>
</template>

<style scoped>
.guide-desc {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-size: 12.5px;
  color: var(--n-text-2);
}

.guide-link {
  white-space: nowrap;
  font-weight: 600;
  color: var(--n-blue);}

.metric-row {
  display: flex;
  flex-wrap: wrap;
}

.metric-row > .ant-col {
  display: flex;
}

.metric-card {
  width: 100%;
  height: 100%;
  box-shadow: var(--n-card-shadow);
  display: flex;
  flex-direction: column;
}

.metric-card :deep(.ant-card-body) {
  height: 100%;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  padding: 12px 14px;
}

.metric-tag-wrap {
  margin-top: 8px;
}

.metric-tag {
  margin-inline-end: 0;
  font-size: 11px;
}

.hero-grid {
  display: grid;
  /* minmax(0,·)：解除 1fr 的内容最小宽度约束，双卡随窗口缩放不横向溢出。 */
  grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr);
  gap: 16px;
}

.hero-card {
  box-shadow: var(--n-card-shadow);
  min-width: 0; /* 卡内 matrix 行才允许收缩换行。 */
}

.hero-status {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 14px;
}

.conn-id-tag {
  font-family: var(--n-mono);
}

.listen-box {
  padding: 12px 14px;
  background: var(--n-bg);
  border: 1px solid var(--n-separator);
  border-radius: var(--n-radius);
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 16px;
}

.listen-group {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.listen-label {
  font-size: 11.5px;
  color: var(--n-text-2);
}

.listen-addr {
  font-family: var(--n-mono);
  font-weight: 700;
  font-size: 14px;
}

.listen-addr.sub-addr {
  font-size: 12.5px;
  font-weight: 500;
  color: var(--n-text-2);
}

.listen-empty {
  font-size: 12.5px;
  color: var(--n-text-3);
}

.hero-actions {
  display: flex;
  gap: 12px;
}

.hero-actions :deep(.ant-btn) {
  flex: 1;
}

/* 中断恢复：主按钮置顶，与下方的"停止"形成明确的主次 */
.relaunch-btn {
  margin-bottom: 8px;
}

.attach-waiting-alert,
.faulted-alert {
  margin-top: 12px;
  text-align: left;
}

.matrix {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.matrix-item {
  display: flex;
  align-items: center;
  flex-wrap: wrap; /* 窄窗口时操作区（切换/按钮）折行到详情下方，不挤压文本 */
  gap: 12px;
  padding: 10px 12px;
  background: var(--n-bg);
  border: 1px solid var(--n-separator);
  border-radius: var(--n-radius);
  transition: border-color var(--n-motion);
}

.matrix-item > .ant-space {
  margin-left: auto; /* 宽窗口时操作区靠右 */
}

.matrix-item:hover {
  border-color: var(--n-separator-strong);
}

.item-icon {
  width: 36px;
  height: 36px;
  border-radius: 9px;
  display: grid;
  place-items: center;
  font-size: 16px;
  flex-shrink: 0;
}

.icon-user {
  background: var(--n-purple-tint);
  color: var(--n-purple);
}

.icon-server {
  background: var(--n-blue-tint);
  color: var(--n-blue);
}

.icon-env {
  background: var(--n-green-tint);
  color: var(--n-green);
}

.item-details {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.item-label {
  font-size: 11px;
  color: var(--n-text-2);
}

.item-val {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  font-size: 13px;
  min-width: 0;
  width: 100%;
}

.item-val strong {
  max-width: 100%;
  /* 长服务器名两行自适应，不硬截断（line-clamp 防溢出）；悬停可看全名。 */
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow: hidden;
  word-break: break-all;
  line-height: 1.35;
}

.item-mono {
  font-family: var(--n-mono);
  font-size: 11.5px;
  color: var(--n-text-2);
}

.item-dir {
  font-size: 11.5px;
  color: var(--n-text-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.item-empty {
  font-size: 12.5px;
  color: var(--n-text-3);
}

.log-terminal {
  min-height: 140px;
  max-height: 220px;
}

@media (max-width: 860px) {
  .hero-grid {
    grid-template-columns: minmax(0, 1fr); /* 单列随窗口缩放 */
  }
}

@media (max-width: 560px) {
  /* 折行后的操作区占满整行，快速切换下拉随行伸缩。 */
  .matrix-item > .ant-space {
    width: 100%;
  }
  .matrix-item > .ant-space .ant-select {
    flex: 1 1 auto;
    min-width: 0;
    max-width: none;
  }
}
</style>
