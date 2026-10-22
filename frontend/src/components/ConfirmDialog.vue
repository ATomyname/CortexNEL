<script setup lang="ts">
// 确认弹窗（架构文档 §10.6）：hosts 劫持（DR-2）与凭据落盘（DR-1）等
// 高风险操作必须走确认弹窗，不可静默执行。薄封装 a-modal，API 与旧版一致。
withDefaults(
  defineProps<{
    open: boolean
    title: string
    message: string
    okText?: string
  }>(),
  { okText: '确认' },
)

const emit = defineEmits<{
  confirm: []
  cancel: []
}>()
</script>

<template>
  <a-modal
    :open="open"
    :title="title"
    :ok-text="okText"
    cancel-text="取消"
    :mask-closable="false"
    :width="420"
    @ok="emit('confirm')"
    @cancel="emit('cancel')"
  >
    <p class="confirm-message">{{ message }}</p>
  </a-modal>
</template>

<style scoped>
.confirm-message {
  margin: 0;
  font-size: 13.5px;
  line-height: 1.75;
  color: var(--n-text-2);
}
</style>
