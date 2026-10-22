<script setup lang="ts">
import { computed, h, onMounted, onUnmounted, ref } from 'vue'
import { EyeOutlined, ReloadOutlined, StopOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import {
  getShadowStatus,
  startShadow,
  stopShadow,
  setShadowSender,
  type ShadowStatus,
} from '../api/shadow'
import { listSessions } from '../api/session'

const status = ref<ShadowStatus | null>(null)
const loading = ref(false)
const starting = ref(false)
const port = ref<number>(25580)
const hasSession = ref(false)
let timer: number | undefined

const stageLabel = computed(() => {
  const s = status.value?.stage || (hasSession.value ? 'UNKNOWN' : '')
  const map: Record<string, string> = {
    IDLE: '空闲',
    ATTACH: '代理就绪',
    TAP: '白端接入',
    AUTH: '登录中',
    MIRROR: '代答完成',
    LIVE: '游戏中',
    CLOSED: '已关闭',
    FAULTED: '异常',
  }
  return map[s] ?? (s ? s : '无会话')
})

const canStart = computed(() => hasSession.value && !status.value?.listening)

const recorderHint = computed(() => {
  const s = status.value
  if (!s) return ''
  if (s.recorderFrames === 0) return '白端尚未进服，暂无可回灌内容'
  if (!s.recorderReady) return '录制缺失世界锚点（预算不足），影子端将拒绝回灌'
  if (s.truncated) return '录制不完整（超出硬上限）'
  return '录制就绪：影子端接入可完整重建世界'
})

async function refresh(): Promise<void> {
  try {
    // 影子端挂在白端会话上（后端 mirrorRun）：没有白端会话就没有可挂靠点。
    const all = await listSessions()
    hasSession.value = all.some((s) => s.launchMirror)
    status.value = await getShadowStatus()
  } catch {
    // 绑定未生成（wails generate 之前）或无会话：静默，页面显示引导文案。
    hasSession.value = false
  }
}

async function onStart(): Promise<void> {
  starting.value = true
  try {
    status.value = await startShadow(port.value)
    message.success(`影子端已开启：${status.value?.addr}`)
  } catch (e) {
    message.error('开启影子端失败: ' + String(e))
  } finally {
    starting.value = false
    void refresh()
  }
}

async function onStop(): Promise<void> {
  loading.value = true
  try {
    status.value = await stopShadow()
    message.info('影子端已停止')
  } catch (e) {
    message.error('停止失败: ' + String(e))
  } finally {
    loading.value = false
    void refresh()
  }
}

async function onSwitchSender(sender: 'A' | 'B'): Promise<void> {
  loading.value = true
  try {
    status.value = await setShadowSender(sender)
    message.success(sender === 'B' ? '影子端已接管上行' : '上行权已交还白端')
  } catch (e) {
    message.error('切换失败: ' + String(e))
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void refresh()
  timer = window.setInterval(() => void refresh(), 3000)
})
onUnmounted(() => {
  if (timer) window.clearInterval(timer)
})
</script>

<template>
  <section class="page">
    <header class="page-header">
      <div>
        <h1 class="page-title">影子端</h1>
        <p class="page-desc">
          让你自己的第二个 Minecraft 客户端接入当前会话：观战白端视角、随时接管操作。
          需要先启动游戏会话（白端进入游戏）。
        </p>
      </div>
      <a-space>
        <a-button :icon="h(ReloadOutlined)" @click="refresh">刷新</a-button>
        <a-button v-if="status?.listening" danger :icon="h(StopOutlined)" :loading="loading" @click="onStop">
          停止影子端
        </a-button>
        <a-button
          v-else
          type="primary"
          :icon="h(EyeOutlined)"
          :loading="starting"
          :disabled="!canStart"
          @click="onStart"
        >
          开启影子端
        </a-button>
      </a-space>
    </header>

    <a-alert
      v-if="!hasSession"
      type="info"
      show-icon
      message="当前没有进行中的游戏会话"
      description="先在「网络服务器」页启动会话并让白端进入游戏，再回到这里开启影子端。"
      style="margin-bottom: 16px"
    />

    <a-row :gutter="16">
      <a-col :span="12">
        <a-card title="连接信息" size="small">
          <template #extra>
            <a-tag v-if="status?.listening" color="green">监听中</a-tag>
            <a-tag v-else color="default">未开启</a-tag>
          </template>
          <a-descriptions :column="1" size="small">
            <a-descriptions-item label="会话阶段">{{ stageLabel }}</a-descriptions-item>
            <a-descriptions-item label="影子地址">
              <span v-if="status?.addr" class="mono">{{ status.addr }}</span>
              <span v-else>—</span>
            </a-descriptions-item>
            <a-descriptions-item label="接入方式">
              在你的客户端多人游戏里直连上面地址（同一台机器 127.0.0.1，局域网用本机 IP）
            </a-descriptions-item>
          </a-descriptions>
          <div v-if="!status?.listening" style="margin-top: 8px">
            <a-input-number v-model:value="port" :min="1024" :max="65535" style="width: 140px" />
            <span class="hint"> 监听端口（默认 25580，与白端代理端口 25565 区分）</span>
          </div>
        </a-card>
      </a-col>

      <a-col :span="12">
        <a-card title="录制与回灌" size="small">
          <template #extra>
            <a-tag v-if="status?.recorderReady" color="green">世界基准就绪</a-tag>
            <a-tag v-else-if="(status?.recorderFrames ?? 0) > 0" color="orange">缺锚点</a-tag>
            <a-tag v-else color="default">等待进服</a-tag>
          </template>
          <a-descriptions :column="1" size="small">
            <a-descriptions-item label="已录制帧数">{{ status?.recorderFrames ?? 0 }}</a-descriptions-item>
            <a-descriptions-item label="录制大小">
              {{ (((status?.recorderBytes ?? 0) / 1024) | 0) }} KB
            </a-descriptions-item>
            <a-descriptions-item label="状态">{{ recorderHint }}</a-descriptions-item>
          </a-descriptions>
        </a-card>
      </a-col>
    </a-row>

    <a-row :gutter="16" style="margin-top: 16px">
      <a-col :span="12">
        <a-card title="操作权（接管开关）" size="small">
          <p class="hint" style="margin-bottom: 12px">
            白端发包（A）：影子端纯观战，任何操作都不会影响游戏。
            影子端接管（B）：你在自己客户端里的操作会生效，白端输入被暂时忽略。
          </p>
          <a-space>
            <a-button
              :type="status?.sender === 'A' || !status?.sender ? 'primary' : 'default'"
              :loading="loading"
              @click="onSwitchSender('A')"
            >
              白端发包（观战）
            </a-button>
            <a-button
              :type="status?.sender === 'B' ? 'primary' : 'default'"
              :danger="status?.sender === 'B'"
              :loading="loading"
              @click="onSwitchSender('B')"
            >
              影子端接管
            </a-button>
          </a-space>
        </a-card>
      </a-col>

      <a-col :span="12">
        <a-card title="诊断计数" size="small">
          <a-descriptions :column="2" size="small">
            <a-descriptions-item label="白端被吞帧">{{ status?.gateStats?.swallowedA ?? 0 }}</a-descriptions-item>
            <a-descriptions-item label="影子被吞帧">{{ status?.gateStats?.swallowedB ?? 0 }}</a-descriptions-item>
            <a-descriptions-item label="KeepAlive 兜底">{{ status?.gateStats?.fallbacks ?? 0 }}</a-descriptions-item>
            <a-descriptions-item label="屏蔽 mod 帧">{{ status?.gateStats?.blockedSB ?? 0 }}</a-descriptions-item>
          </a-descriptions>
        </a-card>
      </a-col>
    </a-row>
  </section>
</template>


<style scoped>
.mono {
  font-family: 'Cascadia Mono', Consolas, monospace;
}
.hint {
  color: var(--text-tertiary, #999);
  font-size: 12px;
}
</style>
