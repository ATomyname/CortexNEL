<!-- 中文逻辑谐音测试卡：从 SettingsView.vue 拆出（规范 §4 文件职责拆分）。
     基础词与父视图的角色命名设置共用同一字段（settings.roleNameBaseWord），
     因此原值经 baseWord 传入、编辑经 update:baseWord 回写，保存由父视图统一落盘。 -->
<script setup lang="ts">
import { ref } from 'vue'
import { TranslationOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { convertHomophone, batchConvertHomophones } from '../api/system'

const props = defineProps<{
  baseWord?: string
  exactTone: boolean
  allowSelf: boolean
}>()

const emit = defineEmits<{
  (e: 'update:baseWord', value: string): void
  (e: 'persist', msg: string): void
  (e: 'convert', word: string): void
  (e: 'batch', word: string): void
  (e: 'copy', value: string): void
}>()

const homophoneLoading = ref(false)
const homophoneBatchLoading = ref(false)
const homophoneResults = ref<string[]>([])

async function onConvertHomophone(): Promise<void> {
  const word = props.baseWord?.trim()
  if (!word) {
    message.warning('请输入要转换的中文基础词')
    return
  }
  homophoneLoading.value = true
  try {
    const res = await convertHomophone(word, props.exactTone, props.allowSelf)
    homophoneResults.value = [res]
    emit('convert', word)
  } catch (e) {
    message.error('转换失败: ' + String(e))
  } finally {
    homophoneLoading.value = false
  }
}

async function onBatchConvertHomophone(): Promise<void> {
  const word = props.baseWord?.trim()
  if (!word) {
    message.warning('请输入要转换的中文基础词')
    return
  }
  homophoneBatchLoading.value = true
  try {
    const list = await batchConvertHomophones(word, 5, props.exactTone, props.allowSelf)
    homophoneResults.value = list
    emit('batch', word)
  } catch (e) {
    message.error('批量生成失败: ' + String(e))
  } finally {
    homophoneBatchLoading.value = false
  }
}
</script>

<template>
  <a-card :bordered="false" class="mb settings-card">
    <template #title>
      <div class="card-title-wrap">
        <TranslationOutlined class="title-icon" />
        <span class="card-title">中文逻辑谐音转换测试</span>
      </div>
    </template>

    <div class="setting-row">
      <div class="setting-info">
        <label class="setting-title">输入基础词 / 原文</label>
        <span class="setting-desc">输入任意中文词组，通过拼音与同音字库逻辑自动转换为谐音</span>
      </div>
      <div class="setting-control" style="flex-direction: column; align-items: flex-end; gap: 8px;">
        <a-input
          :value="baseWord"
          placeholder="输入任意中文，例如：海棠花阔地"
          style="width: 240px"
          @update:value="emit('update:baseWord', $event)"
          @change="emit('persist', '基础词已更新')"
          @blur="emit('persist', '基础词已更新')"
          @pressEnter="onConvertHomophone"
        />
      </div>
    </div>

    <div class="setting-row">
      <div class="setting-info">
        <label class="setting-title">操作操作</label>
        <span class="setting-desc">测试单个谐音或批量抽取 5 个不同同音字组合</span>
      </div>
      <div class="setting-control">
        <a-space :size="10">
          <a-button type="primary" :loading="homophoneLoading" @click="onConvertHomophone">
            生成谐音
          </a-button>
          <a-button :loading="homophoneBatchLoading" @click="onBatchConvertHomophone">
            批量候选 (5个)
          </a-button>
        </a-space>
      </div>
    </div>

    <div class="setting-row" v-if="homophoneResults.length > 0">
      <div class="setting-info">
        <label class="setting-title">谐音转换结果</label>
        <span class="setting-desc">点击任意标签即可直接复制到剪贴板</span>
      </div>
      <div class="setting-control">
        <a-space wrap :size="8">
          <a-tag
            v-for="(res, idx) in homophoneResults"
            :key="idx"
            color="blue"
            style="cursor: pointer; font-size: 13px; padding: 3px 8px;"
            @click="emit('copy', res)"
          >
            {{ res }}
          </a-tag>
        </a-space>
      </div>
    </div>
  </a-card>
</template>
