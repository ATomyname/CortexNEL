<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import zhCN from 'ant-design-vue/es/locale/zh_CN'
import SideBar from './components/SideBar.vue'
import SetupWizard from './components/SetupWizard.vue'
import CommunityModal from './components/CommunityModal.vue'
import InstallProgressBanner from './components/InstallProgressBanner.vue'
import { antdThemeConfig } from './lib/antdTheme'
import { initTheme, themeRef } from './lib/theme'

// 布局骨架：左侧毛玻璃侧栏 + 右侧主内容区。
initTheme()
const themeConfig = computed(() => antdThemeConfig(themeRef().value))

const showCommunity = ref(false)

onMounted(() => {
  if (localStorage.getItem('cortexnel.group.dismissed') !== '1') {
    showCommunity.value = true
  }
})
</script>

<template>
  <a-config-provider :locale="zhCN" :theme="themeConfig">
    <div class="app-shell">
      <SideBar />
      <main class="app-main">
        <router-view />
      </main>
      <SetupWizard />
      <!-- 资产下载进度：全局悬浮，任何页面触发都直接可见 -->
      <InstallProgressBanner />
    </div>

    <!-- 首次启动的官方 Discord 邀请弹窗 -->
    <CommunityModal v-model:open="showCommunity" />
  </a-config-provider>
</template>

<style scoped>
.app-shell {
  display: flex;
  height: 100%;
}

.app-main {
  flex: 1;
  overflow-y: auto;
  min-width: 0;
}
</style>
