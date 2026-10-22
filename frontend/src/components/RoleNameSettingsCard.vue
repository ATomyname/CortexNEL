<!-- 角色随机命名设置卡：从 SettingsView.vue 拆出（规范 §4 文件职责拆分）。
     编辑父视图的响应式 settings 对象切片（roleName* 字段），保存动作经 persist 事件上交，
     由父视图统一落盘（保存策略只有一份，避免各卡片各写一套）。 -->
<script setup lang="ts">
import { ref } from 'vue'
import { ReloadOutlined, UserOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { generateRoleName } from '../api/system'
import type { Settings } from '../api/system'
import { popupContainer } from '../lib/popupContainer'

const props = defineProps<{
  settings: Settings
}>()

const emit = defineEmits<{
  (e: 'persist', msg: string): void
}>()

const previewRoleName = ref('')
const testingGen = ref(false)

async function onTestGenerateRoleName(): Promise<void> {
  testingGen.value = true
  try {
    previewRoleName.value = await generateRoleName({
      prefix: props.settings.roleNamePrefix || '',
      suffix: props.settings.roleNameSuffix || '',
      style: props.settings.roleNameStyle || 'netease',
      baseWord: props.settings.roleNameBaseWord || '',
      digits: props.settings.roleNameDigits ?? 0,
      useHomophone: props.settings.roleNameHomophone ?? false,
      exactTone: props.settings.roleNameExactTone ?? false,
      allowSelf: props.settings.roleNameAllowSelf ?? true,
    })
  } catch (e) {
    message.error('生成失败: ' + String(e))
  } finally {
    testingGen.value = false
  }
}

async function copyPreviewName(): Promise<void> {
  if (!previewRoleName.value) return
  try {
    await navigator.clipboard.writeText(previewRoleName.value)
    message.success('已复制到剪贴板')
  } catch (_) {
    message.info(previewRoleName.value)
  }
}
</script>

<template>
  <a-card :bordered="false" class="mb settings-card">
    <template #title>
      <div class="card-title-wrap">
        <UserOutlined class="title-icon" />
        <span class="card-title">角色随机命名</span>
      </div>
    </template>

    <div class="setting-row">
      <div class="setting-info">
        <label class="setting-title">默认名称前缀</label>
        <span class="setting-desc">生成角色名称时自动附加在最前面的前缀字符（如 Team_、NX_）</span>
      </div>
      <div class="setting-control">
        <a-input
          v-model:value="settings.roleNamePrefix"
          placeholder="无前缀"
          style="width: 160px"
          @change="emit('persist', '前缀已更新')"
          @blur="emit('persist', '前缀已更新')"
        />
      </div>
    </div>

    <div class="setting-row">
      <div class="setting-info">
        <label class="setting-title">默认名称后缀</label>
        <span class="setting-desc">生成角色名称时自动附加在末尾的后缀字符（如 _Pro、_CN）</span>
      </div>
      <div class="setting-control">
        <a-input
          v-model:value="settings.roleNameSuffix"
          placeholder="无后缀"
          style="width: 160px"
          @change="emit('persist', '后缀已更新')"
          @blur="emit('persist', '后缀已更新')"
        />
      </div>
    </div>

    <div class="setting-row">
      <div class="setting-info">
        <label class="setting-title">角色取名模式</label>
        <span class="setting-desc">可选网易官方三段式、简短双段式或中文逻辑谐音模式</span>
      </div>
      <div class="setting-control">
        <a-select
          v-model:value="settings.roleNameStyle"
          :get-popup-container="popupContainer"
          style="width: 200px"
          @change="emit('persist', '取名模式已更新')"
        >
          <a-select-option value="netease">网易官方模式 (三段式)</a-select-option>
          <a-select-option value="short">简短双段式 (形容词+名字)</a-select-option>
          <a-select-option value="homophone">中文逻辑谐音模式</a-select-option>
        </a-select>
      </div>
    </div>

    <!-- 当处于逻辑谐音模式或开启谐音时展示基础词输入 -->
    <div class="setting-row" v-if="settings.roleNameStyle === 'homophone' || settings.roleNameHomophone">
      <div class="setting-info">
        <label class="setting-title">谐音基础词（自定义原词）</label>
        <span class="setting-desc">输入需要转换的起手汉字词组（如“海棠花阔地”，留空则自动从网易官方词库随机抽取词语进行谐音）</span>
      </div>
      <div class="setting-control">
        <a-input
          v-model:value="settings.roleNameBaseWord"
          placeholder="输入自定义基础词，例如：海棠花阔地"
          style="width: 240px"
          @change="emit('persist', '谐音基础词已更新')"
          @blur="emit('persist', '谐音基础词已更新')"
        />
      </div>
    </div>

    <div class="setting-row" v-if="settings.roleNameStyle === 'homophone' || settings.roleNameHomophone">
      <div class="setting-info">
        <label class="setting-title">谐音替换规则</label>
        <span class="setting-desc">严格同调为精确声调匹配；自然保留原字可增加语句自然可读度</span>
      </div>
      <div class="setting-control">
        <a-space :size="16">
          <a-checkbox
            v-model:checked="settings.roleNameExactTone"
            @change="emit('persist', '严格同调设置已更新')"
          >
            严格同声调
          </a-checkbox>
          <a-checkbox
            v-model:checked="settings.roleNameAllowSelf"
            @change="emit('persist', '保留原字设置已更新')"
          >
            自然保留部分原字
          </a-checkbox>
        </a-space>
      </div>
    </div>

    <div class="setting-row">
      <div class="setting-info">
        <label class="setting-title">附加随机数字位数</label>
        <span class="setting-desc">在核心词后追加随机数字（0~4 位，如追加 2 位生成 00~99）</span>
      </div>
      <div class="setting-control">
        <a-select
          v-model:value="settings.roleNameDigits"
          :get-popup-container="popupContainer"
          style="width: 160px"
          @change="emit('persist', '数字位数已更新')"
        >
          <a-select-option :value="0">不添加数字</a-select-option>
          <a-select-option :value="1">1 位随机数 (0~9)</a-select-option>
          <a-select-option :value="2">2 位随机数 (00~99)</a-select-option>
          <a-select-option :value="3">3 位随机数 (000~999)</a-select-option>
          <a-select-option :value="4">4 位随机数 (0000~9999)</a-select-option>
        </a-select>
      </div>
    </div>

    <div class="setting-row" v-if="settings.roleNameStyle !== 'homophone'">
      <div class="setting-info">
        <label class="setting-title">同时应用中文逻辑谐音</label>
        <span class="setting-desc">在官方词库模式下，依然对生成的词语执行同音替换逻辑</span>
      </div>
      <div class="setting-control switch-control">
        <span class="switch-status">
          {{ settings.roleNameHomophone ? '已开启逻辑谐音' : '官方原词输出' }}
        </span>
        <a-switch
          v-model:checked="settings.roleNameHomophone"
          @change="emit('persist', settings.roleNameHomophone ? '逻辑谐音已开启' : '官方原词已恢复')"
        />
      </div>
    </div>

    <div class="setting-row">
      <div class="setting-info">
        <label class="setting-title">生成测试与预览</label>
        <span class="setting-desc">根据当前设置的词库、前缀、后缀及基础词即时测试生成效果</span>
      </div>
      <div class="setting-control">
        <a-space :size="8">
          <code class="mono-code" v-if="previewRoleName">{{ previewRoleName }}</code>
          <a-button :loading="testingGen" @click="onTestGenerateRoleName">
            <template #icon><ReloadOutlined /></template>
            测试生成
          </a-button>
          <a-button v-if="previewRoleName" size="small" @click="copyPreviewName">复制</a-button>
        </a-space>
      </div>
    </div>
  </a-card>
</template>
