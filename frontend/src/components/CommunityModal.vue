<script setup lang="ts">
import { ref } from 'vue'
import { message } from 'ant-design-vue'
import { CopyOutlined, LinkOutlined, TeamOutlined } from '@ant-design/icons-vue'
import { DISCORD_INVITE_URL, openDiscord } from '../lib/external'

defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  (e: 'update:open', val: boolean): void
}>()

const dontShowAgain = ref(false)

function onCopy(): void {
  navigator.clipboard.writeText(DISCORD_INVITE_URL).then(() => {
    message.success('Discord 邀请链接已复制到剪贴板')
  }).catch(() => {
    message.info(`Discord 邀请链接: ${DISCORD_INVITE_URL}`)
  })
}

function onJoin(): void {
  openDiscord()
}

function onClose(): void {
  if (dontShowAgain.value) {
    localStorage.setItem('cortexnel.group.dismissed', '1')
  }
  emit('update:open', false)
}
</script>

<template>
  <a-modal
    :open="open"
    :title="null"
    :footer="null"
    :width="440"
    centered
    class="community-modal"
    @cancel="onClose"
  >
    <div class="community-content">
      <div class="community-header">
        <div class="community-icon-box">
          <TeamOutlined class="community-icon" />
        </div>
        <h2 class="community-title">CortexNEL 官方 Discord</h2>
        <p class="community-subtitle">加入官方 Discord 获取最新版本更新、组件资源与技术答疑</p>
      </div>

      <div class="invite-card" @click="onCopy">
        <div class="invite-info">
          <span class="invite-label">Discord 邀请链接</span>
          <span class="invite-url">{{ DISCORD_INVITE_URL }}</span>
        </div>
        <a-button type="primary" size="small" class="copy-action-btn" @click.stop="onCopy">
          <template #icon><CopyOutlined /></template>
          复制
        </a-button>
      </div>

      <div class="invite-perks">
        <div class="perk-item">
          <span class="perk-dot"></span>
          <span>最新客户端版本发布与更新日志推送</span>
        </div>
        <div class="perk-item">
          <span class="perk-dot"></span>
          <span>网络服与租赁服直连、协议更新解答</span>
        </div>
        <div class="perk-item">
          <span class="perk-dot"></span>
          <span>技术交流、问题反馈与使用答疑</span>
        </div>
      </div>

      <div class="community-actions">
        <a-button type="primary" size="large" class="join-btn" @click="onJoin">
          <template #icon><LinkOutlined /></template>
          加入 Discord
        </a-button>
        <a-button size="large" class="confirm-btn" @click="onClose">
          以后再说
        </a-button>
      </div>

      <div class="modal-footer-tip">
        <a-checkbox v-model:checked="dontShowAgain" class="dont-show-check">
          下次启动不再自动弹出
        </a-checkbox>
      </div>
    </div>
  </a-modal>
</template>

<style scoped>
.community-content {
  padding: 12px 6px 4px;
}

.community-header {
  text-align: center;
  margin-bottom: 20px;
}

.community-icon-box {
  width: 52px;
  height: 52px;
  margin: 0 auto 12px;
  border-radius: 14px;
  background: var(--n-blue-tint);
  color: var(--n-blue);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 26px;
}

.community-title {
  margin: 0 0 6px;
  font-size: 18px;
  font-weight: 700;
  color: var(--n-text-1);
}

.community-subtitle {
  margin: 0;
  font-size: 13px;
  color: var(--n-text-2);
  line-height: 1.5;
}

.invite-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  background: var(--n-bg);
  border: 1px dashed var(--n-separator-strong, rgba(0, 0, 0, 0.2));
  border-radius: 10px;
  padding: 12px 16px;
  margin-bottom: 18px;
  cursor: pointer;
  transition: all 0.2s ease;
}

.invite-card:hover {
  border-color: var(--n-blue);
  background: var(--n-blue-tint);
}

.invite-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.invite-label {
  font-size: 11.5px;
  color: var(--n-text-3);
}

.invite-url {
  font-family: var(--n-mono);
  font-size: 13px;
  font-weight: 600;
  color: var(--n-blue);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.invite-perks {
  display: flex;
  flex-direction: column;
  gap: 8px;
  background: var(--n-bg);
  padding: 12px 14px;
  border-radius: 8px;
  border: 1px solid var(--n-separator);
  margin-bottom: 20px;
}

.perk-item {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12.5px;
  color: var(--n-text-2);
}

.perk-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--n-blue);
  flex-shrink: 0;
}

.community-actions {
  display: flex;
  gap: 12px;
  margin-bottom: 14px;
}

.join-btn,
.confirm-btn {
  flex: 1;
}

.modal-footer-tip {
  display: flex;
  justify-content: center;
}

.dont-show-check {
  font-size: 12px;
  color: var(--n-text-3);
}
</style>
