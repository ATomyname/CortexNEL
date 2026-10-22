<!-- 服务器详情弹窗：从 ServersView.vue 拆出（规范 §4 文件职责拆分）。
     只负责展示与"进入此服务器"的意图，数据由父视图拉取后经 props 传入。 -->
<script setup lang="ts">
import { CompassOutlined } from '@ant-design/icons-vue'
import type { NetworkServerDetail } from '../api/network'

defineProps<{
  open: boolean
  detail: NetworkServerDetail | null
  busy: boolean
  /** 白端会话是否在跑（一键进服单开位被占）。仅代理多开不受此限制。 */
  mirrorRunning: boolean
}>()

const emit = defineEmits<{
  (e: 'cancel'): void
  (e: 'enter'): void
}>()
</script>

<template>
  <a-modal :open="open && !!detail" :title="detail?.name" :width="500" @cancel="emit('cancel')">
    <img v-if="detail?.imageUrl" :src="detail.imageUrl" :alt="detail?.name" class="detail-image" />
    <a-descriptions :column="1" size="small" bordered class="mb">
      <a-descriptions-item label="服务器号 (EntityID)">
        <code class="detail-code">{{ detail?.entityId }}</code>
      </a-descriptions-item>
      <a-descriptions-item label="运行状态">
        <a-badge v-if="detail?.isMaintaining" status="error" text="停服维护中" />
        <a-badge v-else status="success" text="正常运行" />
      </a-descriptions-item>
      <a-descriptions-item label="架构类型">
        <span v-if="detail?.isBedrockOnly">纯基岩版 (Apollo C++)</span>
        <span v-else>Java 版 / 跨端</span>
      </a-descriptions-item>
      <a-descriptions-item label="协议版本">
        {{ detail?.mcVersion || '1.20' }}
      </a-descriptions-item>
    </a-descriptions>
    <h4 class="detail-sub">详细介绍</h4>
    <div class="detail-desc" v-html="detail?.description || '暂无详细介绍信息。'"></div>
    <template #footer>
      <a-button @click="emit('cancel')">关闭</a-button>
      <a-button
        type="primary"
        :disabled="busy || mirrorRunning || detail?.isMaintaining"
        @click="emit('enter')"
      >
        <template #icon><CompassOutlined /></template>
        {{ detail?.isMaintaining ? '服务器维护中' : '进入此服务器' }}
      </a-button>
    </template>
  </a-modal>
</template>

<style scoped>
.detail-image {
  width: 100%;
  max-height: 190px;
  object-fit: cover;
  border-radius: var(--n-radius);
  margin-bottom: 14px;
  border: 1px solid var(--n-separator);
}

.detail-code {
  font-family: var(--n-mono);
}

.detail-sub {
  font-size: 13px;
  font-weight: 600;
  color: var(--n-text-2);
  margin: 0 0 6px;
}

.detail-desc {
  font-size: 13px;
  line-height: 1.7;
  max-height: 200px;
  overflow-y: auto;
  margin: 0;
  color: var(--n-text);
  word-break: break-word;
}

.detail-desc :deep(p) {
  margin: 0 0 6px;
}

.detail-desc :deep(p:last-child) {
  margin-bottom: 0;
}

.detail-desc :deep(strong) {
  font-weight: 700;
}

.detail-desc :deep(img) {
  max-width: 100%;
  height: auto;
  border-radius: 6px;
  margin: 4px 0;
}
</style>
