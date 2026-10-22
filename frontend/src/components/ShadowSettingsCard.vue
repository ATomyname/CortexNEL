<!-- 影子端设置卡：从 SettingsView.vue 拆出（规范 §4 文件职责拆分）。
     编辑父视图的响应式 settings 对象切片（shadowEnabled），保存动作经 persist 事件上交，
     由父视图统一落盘（保存策略只有一份，避免各卡片各写一套）。
     样式自带（scoped）：视图级 scoped 样式不下发到子组件内部元素，自包含才不依赖父作用域。 -->
<script setup lang="ts">
import { VideoCameraOutlined } from '@ant-design/icons-vue'
import type { Settings } from '../api/system'

const props = defineProps<{
  settings: Settings
}>()

const emit = defineEmits<{
  (e: 'persist', msg: string): void
}>()

function onToggle(enabled: boolean): void {
  props.settings.shadowEnabled = enabled
  emit('persist', enabled ? '影子端已开启（新进服会话将录制世界快照）' : '影子端已关闭（新进服会话不再录制）')
}
</script>

<template>
  <a-card :bordered="false" class="mb settings-card">
    <template #title>
      <div class="card-title-wrap">
        <VideoCameraOutlined class="title-icon" />
        <span class="card-title">影子端</span>
      </div>
    </template>

    <div class="setting-row">
      <div class="setting-info">
        <label class="setting-title">启用影子端（世界快照录制与回灌）</label>
        <span class="setting-desc">
          影子端是挂在白端会话上的第二客户端通道，依赖进服过程中的世界快照录制才能回灌。
          不使用影子端时请保持关闭（默认）：会话完全不录制，不占用内存与 CPU。
          此设置仅对新进服的会话生效，已在进行的会话不受影响。
        </span>
      </div>
      <div class="setting-control switch-control">
        <span class="switch-status">
          {{ settings.shadowEnabled ? '已启用（新会话录制）' : '已关闭（新会话不录制）' }}
        </span>
        <a-switch
          :checked="settings.shadowEnabled"
          @change="onToggle($event as boolean)"
        />
      </div>
    </div>
  </a-card>
</template>

<style scoped>
.settings-card {
  box-shadow: var(--n-card-shadow);
}

.card-title-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
}

.title-icon {
  color: var(--n-blue);
  font-size: 16px;
}

.setting-control {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  min-width: 170px;
}

.switch-control {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
}

.switch-status {
  font-size: 12.5px;
  color: var(--n-text-3);
  min-width: 76px;
  text-align: right;
}
</style>
