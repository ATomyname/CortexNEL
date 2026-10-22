<script setup lang="ts">
import { ApiOutlined } from '@ant-design/icons-vue'

// 后端能力未接入的诚实占位面板：只陈述边界与所需工作，不伪装功能。
defineProps<{
  /** 能力名（如「皮肤库」）。 */
  feature: string
  /** 一句话说明落地该能力需要什么（协议事实源/后端工作），给后续实现指路。 */
  requirement: string
}>()
</script>

<template>
  <a-card :bordered="false" class="pending-card">
    <a-result
      status="info"
      :title="feature"
      sub-title="该能力的后端尚未接入，当前页为占位面板，不含任何模拟伪装数据。"
    >
      <template #icon>
        <ApiOutlined class="pending-icon" />
      </template>
      <template #extra>
        <a-tag color="processing">{{ requirement }}</a-tag>
      </template>
    </a-result>
  </a-card>
</template>

<style scoped>
.pending-card {
  max-width: 600px;
  margin: 40px auto;
  box-shadow: var(--n-card-shadow);
}

.pending-icon {
  color: var(--n-blue);
  font-size: 48px;
}
</style>
