<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import {
  FilterOutlined,
  FolderOpenOutlined,
  GlobalOutlined,
  LockOutlined,
  SaveOutlined,
} from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import RoleNameSettingsCard from '../components/RoleNameSettingsCard.vue'
import HomophoneTestCard from '../components/HomophoneTestCard.vue'
import ShadowSettingsCard from '../components/ShadowSettingsCard.vue'
import { useSystemStore } from '../stores/systemStore'
import { getLocalIPv4s, configureFirewall } from '../api/system'
import type { Settings } from '../api/system'

const system = useSystemStore()

const confirmCred = ref(false)
const pendingCred = ref(false)
const saveSuccessMsg = ref('')
const saving = ref(false)

const localIPs = ref<string[]>([])
const firewallLoading = ref(false)

// 响应式表单状态，与全局设置严格双向绑定
const form = reactive<Settings>({
  mainAccountId: '',
  mainEnvId: '',
  mainServerId: '',
  credentialPersist: true,
  hideZeroPlayers: false,
  hideMaintaining: false,
  hideBedrockOnly: false,
  allowLan: false,
  bypassSystemProxy: false,
  shadowEnabled: false,
  lanPort: 25565,
  roleNamePrefix: '',
  roleNameSuffix: '',
  roleNameStyle: 'netease',
  roleNameBaseWord: '海棠花阔地',
  roleNameDigits: 0,
  roleNameHomophone: false,
  roleNameExactTone: false,
  roleNameAllowSelf: true,
})

onMounted(async () => {
  await system.refresh()
  if (system.settings) {
    Object.assign(form, system.settings)
    if (!form.roleNameBaseWord) {
      form.roleNameBaseWord = '海棠花阔地'
    }
    if (!form.lanPort) {
      form.lanPort = 25565
    }
  }
  void loadLocalIPs()
})

async function loadLocalIPs(): Promise<void> {
  try {
    localIPs.value = await getLocalIPv4s()
  } catch (e) {
    console.error('获取本机 IP 失败', e)
  }
}

async function onConfigureFirewall(): Promise<void> {
  firewallLoading.value = true
  try {
    const port = form.lanPort || 25565
    await configureFirewall(port)
    message.success(`已为端口 ${port} 配置 Windows 防火墙入站允许规则`)
  } catch (e) {
    message.error('配置防火墙失败: ' + String(e))
  } finally {
    firewallLoading.value = false
  }
}

function copyText(text: string): void {
  navigator.clipboard.writeText(text).then(() => {
    message.success('已复制: ' + text)
  }).catch(() => {
    message.info('复制失败，请手动选择复制')
  })
}

function notifySave(msg: string): void {
  saveSuccessMsg.value = msg
  setTimeout(() => {
    if (saveSuccessMsg.value === msg) saveSuccessMsg.value = ''
  }, 2500)
}

// 统一持久化方法：将 form 数据完全保存落盘
async function persistSettings(noticeMsg?: string): Promise<void> {
  saving.value = true
  try {
    await system.saveSettings({ ...form })
    notifySave(noticeMsg || '设置已保存')
  } catch (e) {
    message.error('保存设置失败: ' + String(e))
  } finally {
    saving.value = false
  }
}

async function updateFilterSetting(key: 'hideZeroPlayers' | 'hideMaintaining' | 'hideBedrockOnly', val: boolean): Promise<void> {
  form[key] = val
  await persistSettings('服务器列表展示偏好已更新')
}

async function updateLanSetting(val: boolean): Promise<void> {
  form.allowLan = val
  await persistSettings(val ? '局域网共享已开启 (绑定 0.0.0.0)' : '局域网共享已关闭 (仅限本机 127.0.0.1)')
}

async function updateProxySetting(val: boolean): Promise<void> {
  form.bypassSystemProxy = val
  await persistSettings(val ? '已绕过系统代理（直连网易服务）' : '已恢复使用系统代理')
}

function requestCredToggle(checked: boolean): void {
  if (!checked) {
    form.credentialPersist = false
    void persistSettings('凭据落盘已关闭 (仅在当前会话内存保留)')
    return
  }
  pendingCred.value = true
  confirmCred.value = true
}

async function onConfirmCred(): Promise<void> {
  confirmCred.value = false
  form.credentialPersist = true
  await persistSettings('凭据 DPAPI 加密落盘已开启')
}

function onCancelCred(): void {
  confirmCred.value = false
  pendingCred.value = false
}

async function openDataDir(): Promise<void> {
  try {
    await system.openDataDir()
    notifySave('已在系统资源管理器中打开数据目录')
  } catch (err) {
    system.error = String(err)
  }
}
</script>

<template>
  <section class="page">
    <header class="page-header">
      <div>
        <h1 class="page-title">系统设置</h1>
      </div>
      <a-space :size="10">
        <a-button type="primary" :loading="saving" @click="persistSettings('全部设置已保存落盘')">
          <template #icon><SaveOutlined /></template>
          保存设置
        </a-button>
        <a-tag color="success">配置即时生效</a-tag>
      </a-space>
    </header>

    <!-- 保存成功即时反馈 Toast -->
    <a-alert
      v-if="saveSuccessMsg"
      type="success"
      show-icon
      class="mb save-alert"
      :message="saveSuccessMsg"
    />

    <!-- 错误反馈提示横幅 -->
    <a-alert
      v-if="system.error"
      type="error"
      show-icon
      closable
      class="mb"
      :message="system.error"
      @close="system.error = ''"
    />

    <!-- 设置模块 1：账号加密落盘 -->
    <a-card :bordered="false" class="mb settings-card">
      <template #title>
        <div class="card-title-wrap">
          <LockOutlined class="title-icon" />
          <span class="card-title">账号加密落盘</span>
        </div>
      </template>

      <div class="setting-row">
        <div class="setting-info">
          <label class="setting-title">使用 Windows DPAPI 加密保存登录凭据</label>
          <span class="setting-desc">
            启用后在添加或登录账号时使用 Windows DPAPI 加密保存登录凭据（DR-1），在连接异常或会话过期 (0xff) 时用于安全静默重登；关闭时凭据仅在进程运行期间保留于内存中。
          </span>
        </div>
        <div class="setting-control switch-control">
          <span class="switch-status">
            {{ form.credentialPersist ? '已开启保存' : '仅内存保留' }}
          </span>
          <a-switch
            :checked="form.credentialPersist"
            @change="requestCredToggle($event as boolean)"
          />
        </div>
      </div>
    </a-card>

    <!-- 设置模块 2：服务器列表展示与过滤偏好 -->
    <a-card :bordered="false" class="mb settings-card">
      <template #title>
        <div class="card-title-wrap">
          <FilterOutlined class="title-icon" />
          <span class="card-title">服务器列表展示偏好</span>
        </div>
      </template>

      <div class="setting-row">
        <div class="setting-info">
          <label class="setting-title">隐藏 0 人在线服务器</label>
          <span class="setting-desc">开启后自动在网络服务器列表中过滤掉当前在线玩家数为 0 的服务器</span>
        </div>
        <div class="setting-control switch-control">
          <span class="switch-status">
            {{ form.hideZeroPlayers ? '已开启隐藏' : '未开启' }}
          </span>
          <a-switch
            :checked="form.hideZeroPlayers"
            @change="updateFilterSetting('hideZeroPlayers', $event as boolean)"
          />
        </div>
      </div>

      <div class="setting-row">
        <div class="setting-info">
          <label class="setting-title">隐藏停服维护中服务器</label>
          <span class="setting-desc">开启后自动在网络服务器列表中过滤掉处于停机维护或未开放状态的服务器</span>
        </div>
        <div class="setting-control switch-control">
          <span class="switch-status">
            {{ form.hideMaintaining ? '已开启隐藏' : '未开启' }}
          </span>
          <a-switch
            :checked="form.hideMaintaining"
            @change="updateFilterSetting('hideMaintaining', $event as boolean)"
          />
        </div>
      </div>

      <div class="setting-row">
        <div class="setting-info">
          <label class="setting-title">隐藏纯基岩版服务器</label>
          <span class="setting-desc">开启后自动过滤仅支持基岩版（Apollo C++）且不支持 Java 版客户端跨端接入的独立服务器</span>
        </div>
        <div class="setting-control switch-control">
          <span class="switch-status">
            {{ form.hideBedrockOnly ? '已开启隐藏' : '未开启' }}
          </span>
          <a-switch
            :checked="form.hideBedrockOnly"
            @change="updateFilterSetting('hideBedrockOnly', $event as boolean)"
          />
        </div>
      </div>
    </a-card>

    <!-- 设置模块 3：局域网共享 -->
    <a-card :bordered="false" class="mb settings-card">
      <template #title>
        <div class="card-title-wrap">
          <GlobalOutlined class="title-icon" />
          <span class="card-title">局域网共享</span>
        </div>
      </template>

      <div class="setting-row">
        <div class="setting-info">
          <label class="setting-title">开启到局域网（绑定 0.0.0.0）</label>
          <span class="setting-desc">
            开启后本地中继代理将监听所有网络接口（0.0.0.0），局域网内其他设备或虚拟机可通过本机局域网 IP 与端口直连；关闭时仅绑定本机回环（127.0.0.1）。
          </span>
        </div>
        <div class="setting-control switch-control">
          <span class="switch-status">
            {{ form.allowLan ? '已开启 (0.0.0.0)' : '仅本机 (127.0.0.1)' }}
          </span>
          <a-switch
            :checked="form.allowLan"
            @change="updateLanSetting($event as boolean)"
          />
        </div>
      </div>

      <div class="setting-row" v-if="form.allowLan">
        <div class="setting-info">
          <label class="setting-title">局域网代理端口</label>
          <span class="setting-desc">
            代理监听的目标端口，默认 25565（Minecraft 客户端直连标准端口，无需输入端口号）。若被占用将自动使用系统可用端口。
          </span>
        </div>
        <div class="setting-control">
          <a-input-number
            v-model:value="form.lanPort"
            :min="1024"
            :max="65535"
            style="width: 140px"
            placeholder="25565"
            @change="persistSettings('监听端口已更新')"
            @blur="persistSettings('监听端口已更新')"
          />
        </div>
      </div>

      <div class="setting-row" v-if="form.allowLan && localIPs.length > 0">
        <div class="setting-info">
          <label class="setting-title">检测到的本机局域网 IP</label>
          <span class="setting-desc">
            局域网其他电脑或手机上的 Minecraft 请使用此地址直连（切勿在其他设备输入 0.0.0.0）
          </span>
        </div>
        <div class="setting-control">
          <a-space wrap :size="8">
            <a-tag
              v-for="(ip, idx) in localIPs"
              :key="idx"
              color="blue"
              style="cursor: pointer; font-size: 13px; padding: 4px 10px;"
              @click="copyText(`${ip}:${form.lanPort || 25565}`)"
            >
              {{ ip }}:{{ form.lanPort || 25565 }}
            </a-tag>
          </a-space>
        </div>
      </div>

      <div class="setting-row" v-if="form.allowLan">
        <div class="setting-info">
          <label class="setting-title">Windows 防火墙入站放行</label>
          <span class="setting-desc">
            Windows 默认会拦截局域网设备对本机的入站连接。若局域网其他设备提示“连接超时/无法连接”，请点击一键放行端口。
          </span>
        </div>
        <div class="setting-control">
          <a-button :loading="firewallLoading" @click="onConfigureFirewall">
            一键放行防火墙端口
          </a-button>
        </div>
      </div>

      <div class="setting-row">
        <div class="setting-info">
          <label class="setting-title">绕过系统代理</label>
          <span class="setting-desc">
            开启后所有网络请求（网关/登录/组件下载）直连，不再读取系统代理
            （HTTP_PROXY/HTTPS_PROXY）。本地代理（如 Clash）不可达、反复出现
            TLS 握手超时/连接失败时可开启
          </span>
        </div>
        <div class="setting-control switch-control">
          <span class="switch-status">
            {{ form.bypassSystemProxy ? '已开启直连' : '使用系统代理' }}
          </span>
          <a-switch
            :checked="form.bypassSystemProxy"
            @change="updateProxySetting($event as boolean)"
          />
        </div>
      </div>
    </a-card>

    <ShadowSettingsCard :settings="form" @persist="persistSettings" />

    <RoleNameSettingsCard :settings="form" @persist="persistSettings" />

    <HomophoneTestCard
      :base-word="form.roleNameBaseWord"
      :exact-tone="form.roleNameExactTone ?? false"
      :allow-self="form.roleNameAllowSelf ?? true"
      @update:base-word="form.roleNameBaseWord = $event"
      @persist="persistSettings"
      @copy="copyText"
    />

    <!-- 设置模块：数据存储管理 -->
    <a-card :bordered="false" class="settings-card">
      <template #title>
        <div class="card-title-wrap">
          <FolderOpenOutlined class="title-icon" />
          <span class="card-title">数据存储</span>
        </div>
      </template>

      <div class="setting-row">
        <div class="setting-info">
          <label class="setting-title">CortexNEL 数据根目录</label>
          <span class="setting-desc">存储配置文件、加密凭据库与日志文件 (%AppData%\CortexNEL)</span>
        </div>
        <div class="setting-control">
          <a-button class="setting-btn" @click="openDataDir">
            <template #icon><FolderOpenOutlined /></template>
            在资源管理器中打开
          </a-button>
        </div>
      </div>
    </a-card>

    <!-- 凭据二次确认弹窗 -->
    <ConfirmDialog
      :open="confirmCred"
      title="开启凭据加密落盘"
      message="将使用 Windows DPAPI 绑定的当前系统用户密钥加密保存登录凭据，用于会话断开或过期时的快速重连。是否继续开启？"
      @confirm="onConfirmCred"
      @cancel="onCancelCred"
    />
  </section>
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

.save-alert {
  animation: fadeIn 0.2s ease;
}

@keyframes fadeIn {
  from {
    opacity: 0;
    transform: translateY(-4px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
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

.setting-btn {
  min-width: 160px;
}

.mono-code {
  font-family: var(--n-mono);
  font-size: 12px;
  background: var(--n-bg-2);
  padding: 3px 8px;
  border-radius: 4px;
  border: 1px solid var(--n-border);
}

@media (max-width: 768px) {
  .setting-row {
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
  }
  .setting-control {
    width: 100%;
    justify-content: flex-start;
  }
}
</style>
