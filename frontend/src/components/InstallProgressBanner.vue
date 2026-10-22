<script setup lang="ts">
import { useInstallStore } from '../stores/installStore'

// 进服前置资产补齐的全局进度横幅：挂在 App.vue 主界面层，
// 无论当前在哪个页面触发下载（服务器页点进服、概览页、设置页）都直接可见。
// total 未知时展示不确定进度，不编造百分比（规范 U4）。
const install = useInstallStore()
</script>

<template>
  <transition name="install-slide">
    <div v-if="install.active" class="install-banner" role="status">
      <div class="install-head">
        <span class="install-title">正在准备客户端资产</span>
        <a-tag color="processing">{{ install.subject || '本地缺失，正在补齐' }}</a-tag>
      </div>
      <a-progress
        :percent="install.percent ?? undefined"
        :status="install.percent === null ? 'active' : 'normal'"
        :show-info="install.percent !== null"
      />
      <p class="install-label">{{ install.label }}</p>
    </div>
  </transition>
</template>

<style scoped>
.install-banner {
  position: fixed;
  right: 20px;
  bottom: 20px;
  z-index: 1100;
  width: 360px;
  padding: 14px 16px;
  border-radius: 12px;
  background: var(--n-bg);
  box-shadow: 0 8px 28px rgba(0, 0, 0, 0.22);
  border: 1px solid rgba(255, 255, 255, 0.08);
}

.install-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.install-title {
  font-weight: 600;
}

.install-label {
  margin: 6px 0 0;
  color: var(--n-text-secondary, #8a8f99);
  font-size: 12px;
}

.install-slide-enter-active,
.install-slide-leave-active {
  transition: transform 0.25s ease, opacity 0.25s ease;
}

.install-slide-enter-from,
.install-slide-leave-to {
  transform: translateY(12px);
  opacity: 0;
}
</style>
