<script setup lang="ts">
import { computed } from 'vue'

/**
 * 6 大核心连接状态体系（与 internal/session/state.go 的阶段定义严格对应）：
 * 1. IDLE / CLOSED: 未运行
 * 2. ATTACH: 代理已监听，等待客户端连入（用户等得最久的阶段在这一步）
 * 3. TAP: 客户端已连入
 * 4. AUTH / MIRROR: 认证中 (代答完成)
 * 5. LIVE: 已连接 (双向转发就绪)
 * 6. FAULTED / LOST: 连接中断
 */
const props = withDefaults(
  defineProps<{
    stage?: string
    active?: boolean
    size?: 'sm' | 'md' | 'lg'
    showLabel?: boolean
  }>(),
  {
    stage: 'IDLE',
    active: false,
    size: 'md',
    showLabel: true,
  },
)

interface StatusMeta {
  key: string
  label: string
  desc: string
  /** a-tag 色板名。 */
  color: string
  pulse: boolean
}

const status = computed<StatusMeta>(() => {
  const s = (props.stage || 'IDLE').toUpperCase()
  if (!props.active && (s === 'IDLE' || s === 'CLOSED' || s === '')) {
    return { key: 'idle', label: '未运行', desc: '代理与会话处于空闲状态', color: 'default', pulse: false }
  }

  switch (s) {
    case 'ATTACH':
      return { key: 'attach', label: '等待客户端', desc: '本地代理已监听，等待 Minecraft 客户端连入本地端口', color: 'processing', pulse: true }
    case 'TAP':
      return { key: 'tap', label: '客户端已接入', desc: '客户端已连入本地代理，正在向上游发起连接', color: 'warning', pulse: true }
    case 'AUTH':
    case 'MIRROR':
      return { key: 'auth', label: '认证中', desc: '正在向上游服务器验证凭据并执行握手', color: 'purple', pulse: true }
    case 'LIVE':
      return { key: 'live', label: '已连接', desc: 'Play 双向转发已建立，游戏进行中', color: 'success', pulse: false }
    case 'FAULTED':
      return { key: 'faulted', label: '连接中断', desc: '网络上游断开或代理异常中断', color: 'error', pulse: false }
    default:
      if (props.active) {
        return { key: 'active', label: `运行中 · ${s}`, desc: `当前阶段: ${s}`, color: 'processing', pulse: true }
      }
      return { key: 'idle', label: '未运行', desc: '未启动本地代理', color: 'default', pulse: false }
  }
})
</script>

<template>
  <a-tooltip :title="status.desc">
    <a-tag class="status-badge" :class="[`size-${size}`, `status-${status.key}`]" :color="status.color">
      <span class="status-indicator">
        <span class="status-dot"></span>
        <span v-if="status.pulse" class="status-ring"></span>
      </span>
      <span v-if="showLabel" class="status-label">{{ status.label }}</span>
    </a-tag>
  </a-tooltip>
</template>

<style scoped>
.status-badge {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  white-space: nowrap;
  user-select: none;
}

.size-sm {
  font-size: 11px;
  padding: 0 7px;
  gap: 5px;
}

.size-md {
  font-size: 12.5px;
  padding: 2px 10px;
  gap: 7px;
}

.size-lg {
  font-size: 14px;
  padding: 4px 14px;
  gap: 9px;
}

.status-indicator {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 8px;
  height: 8px;
}

.size-sm .status-indicator {
  width: 6px;
  height: 6px;
}

.size-lg .status-indicator {
  width: 10px;
  height: 10px;
}

.status-dot {
  width: 100%;
  height: 100%;
  border-radius: 50%;
  background: currentColor;
}

.status-ring {
  position: absolute;
  inset: -3px;
  border-radius: 50%;
  border: 1.5px solid currentColor;
  opacity: 0.75;
  animation: pulse-ring 2s cubic-bezier(0.24, 0, 0.38, 1) infinite;
}

@keyframes pulse-ring {
  0% {
    transform: scale(0.85);
    opacity: 0.8;
  }
  50% {
    transform: scale(1.6);
    opacity: 0;
  }
  100% {
    transform: scale(0.85);
    opacity: 0;
  }
}
</style>
