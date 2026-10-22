<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { FolderOpenOutlined, ReloadOutlined, ToolOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { listPlugins, togglePlugin, openModsDir, getClientEnvInfo, type Plugin, type EnvInfo } from '../api/plugins'

const plugins = ref<Plugin[]>([])
const envInfo = ref<EnvInfo | null>(null)
const loading = ref(false)
const error = ref('')
const keyword = ref('')

const filtered = computed(() => {
  const k = keyword.value.trim().toLowerCase()
  return k
    ? plugins.value.filter((p) => `${p.name} ${p.iid} ${p.fileName}`.toLowerCase().includes(k))
    : plugins.value
})

const columns = [
  { key: 'name', title: '组件 / 模组', minWidth: 180 },
  { key: 'fileName', title: '文件名', width: 220, ellipsis: true },
  { key: 'type', title: '类型', width: 140 },
  { key: 'status', title: '状态', width: 95 },
  { key: 'actions', title: '操作', width: 95 },
]

async function refresh(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const [pList, env] = await Promise.all([
      listPlugins(),
      getClientEnvInfo().catch(() => null),
    ])
    plugins.value = pList
    envInfo.value = env
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

async function onOpenModsDir(): Promise<void> {
  try {
    await openModsDir()
    message.success('已在系统资源管理器中打开 Mods 目录')
  } catch (e) {
    message.error('打开 Mods 目录失败: ' + String(e))
  }
}

async function toggle(plugin: Plugin): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    await togglePlugin(plugin.fileName)
    await refresh()
  } catch (e) {
    error.value = String(e)
    loading.value = false
  }
}

onMounted(() => {
  void refresh()
})
</script>

<template>
  <section class="page">
    <header class="page-header">
      <div>
        <h1 class="page-title">白端设置</h1>
        <p class="page-desc">管理本地白端 Minecraft 客户端环境路径、Java 运行时与 Mods 模组组件</p>
      </div>
      <a-space>
        <a-input-search
          v-model:value="keyword"
          placeholder="搜索组件名称 / 文件名..."
          style="width: 220px"
          allow-clear
        />
        <a-button @click="onOpenModsDir">
          <template #icon><FolderOpenOutlined /></template>
          打开 Mods 目录
        </a-button>
        <a-button :loading="loading" @click="refresh">
          <template #icon><ReloadOutlined /></template>
          刷新
        </a-button>
      </a-space>
    </header>

    <a-alert
      v-if="error"
      type="error"
      show-icon
      closable
      class="mb"
      :message="error"
      @close="error = ''"
    />

    <!-- 白端环境信息卡片 -->
    <a-card :bordered="false" class="mb env-card">
      <template #title>
        <div class="env-title">
          <ToolOutlined />
          <span>客户端环境信息</span>
        </div>
      </template>
      <a-descriptions :column="{ xxl: 2, xl: 2, lg: 1, md: 1, sm: 1, xs: 1 }" size="small" bordered>
        <a-descriptions-item label="游戏客户端根目录">
          <code class="env-path">{{ envInfo?.clientDir || '正在自动探测...' }}</code>
        </a-descriptions-item>
        <a-descriptions-item label="Mods 模组目录">
          <code class="env-path">{{ envInfo?.modsDir || '正在自动探测...' }}</code>
        </a-descriptions-item>
        <a-descriptions-item label="Java 运行环境">
          <code class="env-path">{{ envInfo?.javaPath || '系统自动探测推荐环境' }}</code>
        </a-descriptions-item>
        <a-descriptions-item label="环境托管状态">
          <a-badge status="processing" text="全自动探测装配（免手动配置）" />
        </a-descriptions-item>
      </a-descriptions>
    </a-card>

    <a-card :bordered="false" class="plugins-card">
      <template #title><span class="card-title">Mods 与本地组件列表</span></template>
      <template #extra><a-tag color="blue">共 {{ plugins.length }} 个组件</a-tag></template>

      <a-table
        :data-source="filtered"
        :columns="columns"
        row-key="fileName"
        :loading="loading"
        :pagination="false"
        :scroll="{ x: 720 }"
      >
        <template #bodyCell="{ column, record }: { column: { key: string }, record: Plugin }">
          <template v-if="column.key === 'name'">
            <div class="plugin-cell">
              <strong class="plugin-name">{{ record.name }}</strong>
            </div>
          </template>

          <template v-else-if="column.key === 'fileName'">
            <code class="plugin-file">{{ record.fileName }}</code>
          </template>

          <template v-else-if="column.key === 'type'">
            <a-tag :color="record.iid ? 'purple' : 'default'">
              {{ record.iid ? '网易组件 (' + record.iid + ')' : '标准 Mod' }}
            </a-tag>
          </template>

          <template v-else-if="column.key === 'status'">
            <a-badge
              :status="record.enabled ? 'success' : 'default'"
              :text="record.enabled ? '已启用' : '已停用'"
            />
          </template>

          <template v-else-if="column.key === 'actions'">
            <a-switch
              :checked="record.enabled"
              :disabled="loading"
              checked-children="启用"
              un-checked-children="停用"
              @change="toggle(record)"
            />
          </template>
        </template>

        <template #emptyText>
          <a-empty description="当前客户端 mods 目录下暂无模组组件，可通过右上角「打开 Mods 目录」放入 .jar 文件。" />
        </template>
      </a-table>
    </a-card>
  </section>
</template>

<style scoped>
.env-card,
.plugins-card {
  box-shadow: var(--n-card-shadow);
  overflow: hidden;
}

.env-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
}

.env-path {
  font-family: var(--n-mono);
  font-size: 12px;
  word-break: break-all;
}

:deep(.ant-table-cell) {
  white-space: nowrap;
}

.plugin-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}

.plugin-name {
  font-size: 13.5px;
}

.plugin-file {
  font-family: var(--n-mono);
  font-size: 12px;
}
</style>
