<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useAccountStore } from '../stores/accountStore'
import { useProfileStore } from '../stores/profileStore'
import { useSystemStore } from '../stores/systemStore'

const accounts = useAccountStore(); const profiles = useProfileStore(); const system = useSystemStore()
const dismissed = ref(false); const ready = ref(false)
const steps = computed(() => [
  { key: 'account', label: '登录账号', to: '/accounts', done: accounts.accounts.length > 0 },
  { key: 'server', label: '选择服务器', to: '/servers', done: false },
])
const visible = computed(() => ready.value && !dismissed.value && accounts.accounts.length === 0)
const nextStep = computed(() => steps.value.find((step) => !step.done) ?? steps.value[0])
onMounted(async () => {
  dismissed.value = localStorage.getItem('cortexnel.setup.dismissed') === '1'
  if (!(window as Window & { go?: unknown }).go) return
  await Promise.all([accounts.refresh(), profiles.refresh(), system.refresh()])
  ready.value = true
})
function close(): void { dismissed.value = true; localStorage.setItem('cortexnel.setup.dismissed', '1') }
</script>

<template>
  <a-modal :open="visible" title="完成设置后即可进服" :width="430" :footer="null" @cancel="close">
    <p class="wizard-eyebrow">首次设置</p>
    <p class="wizard-sub">按顺序完成基础配置，之后可在概览页一键启动。</p>
    <a-steps direction="vertical" size="small" :current="steps.findIndex((s) => !s.done)">
      <a-step
        v-for="step in steps"
        :key="step.key"
        :title="step.label"
        :status="step.done ? 'finish' : step.key === nextStep.key ? 'process' : 'wait'"
      >
        <template #description>
          <router-link v-if="step.key === nextStep.key && !step.done" :to="step.to" @click="close">去设置 →</router-link>
          <span v-else-if="step.done" class="wizard-done">已完成</span>
        </template>
      </a-step>
    </a-steps>
    <div class="wizard-actions">
      <a-button @click="close">稍后再说</a-button>
      <router-link :to="nextStep.to" @click="close">
        <a-button type="primary">{{ nextStep.done ? '查看设置' : '继续' }}</a-button>
      </router-link>
    </div>
  </a-modal>
</template>

<style scoped>
.wizard-eyebrow {
  margin: 0 0 4px;
  color: var(--n-blue);
  font-size: 12px;
  font-weight: 600;
}

.wizard-sub {
  margin: 0 0 16px;
  color: var(--n-text-2);
  font-size: 13px;
}

.wizard-done {
  font-size: 12px;
  color: var(--n-green);
}

.wizard-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 18px;
}
</style>
