<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { CheckCircleOutlined, SkinOutlined, SyncOutlined } from '@ant-design/icons-vue'
import { useSkinStore } from '../stores/skinStore'

const skins = useSkinStore()
const keyword = ref('')

onMounted(() => {
  void skins.refresh()
})

function search(): void {
  void skins.refresh(keyword.value.trim())
}
</script>

<template>
  <section class="page">
    <header class="page-header">
      <div>
        <h1 class="page-title">皮肤管理</h1>
        <p class="page-desc">浏览并一键应用网易官方免费皮肤资源（未配置皮肤时自动随机匹配）</p>
      </div>
      <a-space>
        <a-input-search
          v-model:value="keyword"
          placeholder="搜索皮肤名称..."
          style="width: 240px"
          allow-clear
          @search="search"
        />
        <a-button :loading="skins.loading" @click="search">搜索</a-button>
        <a-button type="primary" :loading="skins.loading" @click="skins.randomize()">
          <template #icon><SyncOutlined /></template>
          随机换一套
        </a-button>
      </a-space>
    </header>

    <a-alert
      type="info"
      show-icon
      class="mb"
      message="智能皮肤托管已就绪：当账号未配置任何皮肤时，CortexNEL 会自动在进服与登录时为您随机挑选一套免费皮肤。"
    />

    <a-alert v-if="skins.error" type="warning" show-icon class="mb" :message="skins.error" />
    <a-alert v-if="skins.applied" type="success" show-icon class="mb" message="皮肤已成功应用，请重新进入游戏后生效。" />

    <a-card :bordered="false" class="skin-panel">
      <div v-if="skins.loading" class="center-spin">
        <a-spin tip="正在加载皮肤资源..." />
      </div>

      <a-empty v-else-if="!skins.skins.length" description="暂无匹配的皮肤结果" />

      <div v-else class="skin-grid">
        <a-card
          v-for="skin in skins.skins"
          :key="skin.entityId"
          hoverable
          size="small"
          class="skin-card"
        >
          <template #cover>
            <img v-if="skin.previewUrl" :src="skin.previewUrl" :alt="skin.name" class="skin-img" loading="lazy" />
            <div v-else class="skin-placeholder">
              <SkinOutlined class="skin-icon" />
              <span>CortexNEL</span>
            </div>
          </template>
          <div class="skin-meta">
            <strong class="skin-name">{{ skin.name || '未命名皮肤' }}</strong>
            <span class="skin-id">#{{ skin.entityId }}</span>
          </div>
          <template #actions>
            <a-button
              v-if="skins.currentSkin?.entityId === skin.entityId"
              type="text"
              block
              style="color: var(--n-green); cursor: default;"
            >
              <template #icon><CheckCircleOutlined /></template>
              使用中
            </a-button>
            <a-button
              v-else
              type="link"
              block
              :disabled="skins.loading"
              @click="skins.apply(skin.entityId)"
            >
              应用皮肤
            </a-button>
          </template>
        </a-card>
      </div>
    </a-card>
  </section>
</template>

<style scoped>
.skin-panel {
  box-shadow: var(--n-card-shadow);
}

.center-spin {
  display: grid;
  place-items: center;
  padding: 40px 0;
}

.skin-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 16px;
}

.skin-card {
  border-radius: var(--n-radius);
  overflow: hidden;
  border: 1px solid var(--n-separator);
}

.skin-img,
.skin-placeholder {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  background: var(--n-surface-subtle);
}

.skin-placeholder {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  color: var(--n-text-3);
  font-size: 12px;
}

.skin-icon {
  font-size: 28px;
}

.skin-meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 4px 0;
}

.skin-name {
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.skin-id {
  font-size: 11px;
  color: var(--n-text-3);
  font-family: var(--n-mono);
}
</style>
