<script setup lang="ts">
/**
 * 添加账号弹窗：四条登录/导入入口（4399 密码、网易 mPay、Cookie、小号机）。
 * 从 AccountsView 拆出——账号页只保留列表与编辑，登录表单的状态与流程独立在本组件。
 */
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import {
  KeyOutlined,
  LoadingOutlined,
  MobileOutlined,
  SafetyOutlined,
  UserOutlined,
} from '@ant-design/icons-vue'
import SafePasswordInput from './SafePasswordInput.vue'
import { useAccountStore } from '../stores/accountStore'
import { useSystemStore } from '../stores/systemStore'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ (e: 'update:open', value: boolean): void }>()

const accounts = useAccountStore()
const system = useSystemStore()

const visible = computed({
  get: () => props.open,
  set: (value: boolean) => emit('update:open', value),
})

const tab = ref<'password' | 'x19' | 'cookie' | 'smallmachine'>('password')
const addError = ref('')
const loginStatus = ref('')

// 4399 账号密码
const accName = ref('')
const accPassword = ref('')

// Cookie 导入
const cookieInput = ref('')
const loginLabel = ref('')
const smallMachineInput = ref('')

// x19 邮箱登录表单状态
const x19Email = ref('')
const x19Password = ref('')
const x19Phone = ref('')
const x19SmsCode = ref('')
const x19Mode = ref<'email' | 'phonePassword' | 'sms'>('email')
const x19SmsCooldown = ref(0)
let x19SmsTimer: number | undefined

onBeforeUnmount(() => {
  if (x19SmsTimer !== undefined) window.clearInterval(x19SmsTimer)
})

/** 每次打开都回到干净状态，避免上一次的输入或报错残留。 */
watch(
  () => props.open,
  (open) => {
    if (!open) return
    tab.value = 'password'
    addError.value = ''
    loginStatus.value = ''
    accName.value = ''
    accPassword.value = ''
    cookieInput.value = ''
    loginLabel.value = ''
    smallMachineInput.value = ''
    x19Email.value = ''
    x19Password.value = ''
    x19Phone.value = ''
    x19SmsCode.value = ''
    x19Mode.value = 'email'
    x19SmsCooldown.value = 0
  },
  { immediate: true },
)

function switchTab(t: 'password' | 'x19' | 'cookie' | 'smallmachine'): void {
  tab.value = t
  addError.value = ''
  loginStatus.value = ''
}

function errMsg(e: unknown): string {
  if (e instanceof Error) return e.message
  const str = String(e)
  if (str.includes('4399')) return '4399 账号认证失败: ' + str
  if (str.includes('captcha') || str.includes('验证码')) return '验证码识别超时或失败，已自动重试后仍未通过，请检查账号密码'
  return str
}

function startX19SmsCooldown(): void {
  x19SmsCooldown.value = 60
  if (x19SmsTimer !== undefined) window.clearInterval(x19SmsTimer)
  x19SmsTimer = window.setInterval(() => {
    x19SmsCooldown.value -= 1
    if (x19SmsCooldown.value <= 0 && x19SmsTimer !== undefined) {
      window.clearInterval(x19SmsTimer)
      x19SmsTimer = undefined
    }
  }, 1000)
}

async function onRequestX19SMS(): Promise<void> {
  if (!x19Phone.value.trim()) {
    addError.value = '请输入手机号'
    return
  }
  addError.value = ''
  loginStatus.value = '正在发送短信验证码...'
  try {
    await accounts.requestX19SMS(x19Phone.value.trim())
    startX19SmsCooldown()
    loginStatus.value = '验证码已发送'
  } catch (e) {
    addError.value = errMsg(e)
    loginStatus.value = ''
  }
}

async function onLoginX19(): Promise<void> {
  if (x19Mode.value === 'email' && (!x19Email.value.trim() || !x19Password.value)) {
    addError.value = '请输入网易邮箱账号与密码'
    return
  }
  if (x19Mode.value !== 'email' && (!x19Phone.value.trim() || (x19Mode.value === 'phonePassword' ? !x19Password.value : !x19SmsCode.value.trim()))) {
    addError.value = x19Mode.value === 'phonePassword' ? '请输入手机号和密码' : '请输入手机号和短信验证码'
    return
  }
  addError.value = ''
  loginStatus.value = '正在注册 mPay 设备并执行邮箱登录链...'
  try {
    const isPersist = system.settings?.credentialPersist ?? false
    if (x19Mode.value === 'email') {
      await accounts.loginX19(x19Email.value.trim(), x19Password.value, loginLabel.value.trim(), isPersist)
    } else if (x19Mode.value === 'phonePassword') {
      await accounts.loginX19PhonePassword(x19Phone.value.trim(), x19Password.value, loginLabel.value.trim(), isPersist)
    } else {
      await accounts.loginX19PhoneSMS(x19Phone.value.trim(), x19SmsCode.value.trim(), loginLabel.value.trim(), isPersist)
    }
    x19Email.value = ''
    x19Password.value = ''
    x19Phone.value = ''
    x19SmsCode.value = ''
    loginStatus.value = '登录成功'
    message.success('网易邮箱账号添加成功')
    setTimeout(() => {
      visible.value = false
      loginStatus.value = ''
    }, 600)
  } catch (e) {
    addError.value = errMsg(e)
    loginStatus.value = ''
  }
}

async function onLogin4399(): Promise<void> {
  if (!accName.value.trim() || !accPassword.value) {
    addError.value = '请输入 4399 账号与密码'
    return
  }
  addError.value = ''
  loginStatus.value = '正在连接认证服务器，自动识别验证码中...'
  try {
    const isPersist = system.settings?.credentialPersist ?? false
    await accounts.login4399(accName.value.trim(), accPassword.value, '', isPersist)
    accName.value = ''
    accPassword.value = ''
    loginStatus.value = '登录成功'
    message.success('4399 账号添加成功')
    setTimeout(() => {
      visible.value = false
      loginStatus.value = ''
    }, 600)
  } catch (e) {
    addError.value = errMsg(e)
    loginStatus.value = ''
  }
}

async function onLoginCookie(): Promise<void> {
  if (!cookieInput.value.trim()) {
    addError.value = '请粘贴有效 sauth Cookie 内容'
    return
  }
  addError.value = ''
  loginStatus.value = '正在校验 Cookie 凭据有效性...'
  try {
    const isPersist = system.settings?.credentialPersist ?? false
    await accounts.login(loginLabel.value.trim(), cookieInput.value.trim(), isPersist)
    cookieInput.value = ''
    loginLabel.value = ''
    loginStatus.value = '导入成功'
    message.success('Cookie 凭据导入成功')
    setTimeout(() => {
      visible.value = false
      loginStatus.value = ''
    }, 600)
  } catch (e) {
    addError.value = errMsg(e)
    loginStatus.value = ''
  }
}

async function onImportSmallMachine(): Promise<void> {
  if (!smallMachineInput.value.trim()) {
    addError.value = '请粘贴小号机导出内容'
    return
  }
  addError.value = ''
  loginStatus.value = '正在解析小号机数据并登录...'
  try {
    const isPersist = system.settings?.credentialPersist ?? false
    await accounts.importSmallMachine(smallMachineInput.value.trim(), isPersist)
    smallMachineInput.value = ''
    loginStatus.value = '导入成功'
    message.success('小号机账号导入成功')
    setTimeout(() => {
      visible.value = false
      loginStatus.value = ''
    }, 600)
  } catch (e) {
    addError.value = errMsg(e)
    loginStatus.value = ''
  }
}
</script>

<template>
  <a-modal
    v-model:open="visible"
    title="添加新账号"
    :footer="null"
    :width="540"
    destroy-on-close
  >
    <div class="modal-body">
      <a-tabs v-model:activeKey="tab" @tab-click="switchTab($event as 'password' | 'x19' | 'cookie' | 'smallmachine')">
        <!-- Tab 1: 4399 密码登录 -->
        <a-tab-pane key="password" tab="4399 账号密码登录">
          <a-alert v-if="loginStatus" type="info" show-icon class="mb" :message="loginStatus">
            <template #icon><LoadingOutlined spin /></template>
          </a-alert>
          <a-alert v-if="addError" type="warning" show-icon closable class="mb" :message="addError" @close="addError = ''" />

          <form @submit.prevent="onLogin4399">
            <div class="form-col">
              <label class="form-label">4399 账号 / 手机号</label>
              <a-input
                v-model:value="accName"
                placeholder="输入 4399 账号或绑定的手机号"
                :disabled="accounts.loading"
                autocomplete="username"
              >
                <template #prefix><UserOutlined class="input-icon" /></template>
              </a-input>

              <label class="form-label">登录密码</label>
              <SafePasswordInput
                v-model="accPassword"
                placeholder="输入 4399 登录密码"
                :disabled="accounts.loading"
                @enter="onLogin4399"
              />

              <div class="form-actions">
                <a-button type="primary" size="large" html-type="submit" block :loading="accounts.loading">
                  <template #icon><KeyOutlined /></template>
                  {{ accounts.loading ? '正在安全登录与自动识别验证码...' : '立即登录' }}
                </a-button>
                <span class="security-hint">
                  <SafetyOutlined class="hint-icon" />
                  <span>密码直接经由安全通道递交验证，绝不会明文展示或记录到客户端本地日志中</span>
                </span>
              </div>
            </div>
          </form>
        </a-tab-pane>

        <!-- Tab 2: x19 邮箱登录 -->
        <a-tab-pane key="x19" tab="网易 mPay 登录">
          <a-alert v-if="loginStatus" type="info" show-icon class="mb" :message="loginStatus">
            <template #icon><LoadingOutlined spin /></template>
          </a-alert>
          <a-alert v-if="addError" type="warning" show-icon closable class="mb" :message="addError" @close="addError = ''" />

          <a-radio-group v-model:value="x19Mode" button-style="solid" class="x19-mode">
            <a-radio-button value="email">邮箱密码</a-radio-button>
            <a-radio-button value="phonePassword">手机号密码</a-radio-button>
            <a-radio-button value="sms">短信验证码</a-radio-button>
          </a-radio-group>

          <form @submit.prevent="onLoginX19">
            <div class="form-col">
              <template v-if="x19Mode === 'email'">
                <label class="form-label">网易邮箱账号</label>
                <a-input
                  v-model:value="x19Email"
                  placeholder="网易邮箱地址（如 xxx@163.com）"
                  :disabled="accounts.loading"
                  autocomplete="username"
                >
                  <template #prefix><UserOutlined class="input-icon" /></template>
                </a-input>

                <label class="form-label">邮箱密码</label>
                <SafePasswordInput
                  v-model="x19Password"
                  placeholder="输入网易邮箱密码"
                  :disabled="accounts.loading"
                  @enter="onLoginX19"
                />
              </template>

              <template v-else>
                <label class="form-label">手机号</label>
                <a-input
                  v-model:value="x19Phone"
                  placeholder="输入网易账号绑定的手机号"
                  :disabled="accounts.loading"
                  autocomplete="tel"
                >
                  <template #prefix><MobileOutlined class="input-icon" /></template>
                </a-input>
              </template>

              <template v-if="x19Mode === 'phonePassword'">
                <label class="form-label">手机号密码</label>
                <SafePasswordInput
                  v-model="x19Password"
                  placeholder="输入手机号登录密码"
                  :disabled="accounts.loading"
                  @enter="onLoginX19"
                />
              </template>

              <template v-else-if="x19Mode === 'sms'">
                <label class="form-label">短信验证码</label>
                <a-space-compact block>
                  <a-input
                    v-model:value="x19SmsCode"
                    placeholder="输入短信验证码"
                    :disabled="accounts.loading"
                    autocomplete="one-time-code"
                  />
                  <a-button
                    html-type="button"
                    :disabled="accounts.loading || x19SmsCooldown > 0"
                    @click="onRequestX19SMS"
                  >
                    {{ x19SmsCooldown > 0 ? `${x19SmsCooldown}s 后重发` : '发送验证码' }}
                  </a-button>
                </a-space-compact>
              </template>

              <div class="form-actions">
                <a-button type="primary" size="large" html-type="submit" block :loading="accounts.loading">
                  <template #icon><KeyOutlined /></template>
                  {{ accounts.loading ? '正在执行 mPay 登录链...' : '立即登录' }}
                </a-button>
                <span class="security-hint">
                  <SafetyOutlined class="hint-icon" />
                  <span>走网易官方 mPay 通道登录，密码仅用于本次登录请求</span>
                </span>
              </div>
            </div>
          </form>
        </a-tab-pane>

        <!-- Tab 3: Cookie 导入 -->
        <a-tab-pane key="cookie" tab="Cookie 凭据导入">
          <a-alert v-if="loginStatus" type="info" show-icon class="mb" :message="loginStatus">
            <template #icon><LoadingOutlined spin /></template>
          </a-alert>
          <a-alert v-if="addError" type="warning" show-icon closable class="mb" :message="addError" @close="addError = ''" />

          <form @submit.prevent="onLoginCookie">
            <div class="form-col">
              <label class="form-label">账号备注（可选）</label>
              <a-input
                v-model:value="loginLabel"
                placeholder="例如：主力大号 / 备用小号"
                :disabled="accounts.loading"
              />

              <label class="form-label">sauth Cookie 内容</label>
              <a-textarea
                v-model:value="cookieInput"
                :rows="4"
                placeholder="粘贴从抓包或浏览器获取的 sauth Cookie 文本..."
                :disabled="accounts.loading"
              />

              <div class="form-actions">
                <a-button type="primary" size="large" html-type="submit" block :loading="accounts.loading">
                  <template #icon><KeyOutlined /></template>
                  {{ accounts.loading ? '正在解析导入...' : '导入账号凭据' }}
                </a-button>
              </div>
            </div>
          </form>
        </a-tab-pane>

        <!-- Tab 4：小号机一键导入 -->
        <a-tab-pane key="smallmachine" tab="小号机一键导入">
          <a-alert v-if="loginStatus" type="info" show-icon class="mb" :message="loginStatus">
            <template #icon><LoadingOutlined spin /></template>
          </a-alert>
          <a-alert v-if="addError" type="warning" show-icon closable class="mb" :message="addError" @close="addError = ''" />

          <form @submit.prevent="onImportSmallMachine">
            <div class="form-col">
              <label class="form-label">粘贴小号机导出内容</label>
              <a-textarea
                v-model:value="smallMachineInput"
                :rows="9"
                placeholder="Channel: 4399mixed&#10;账号: your-account&#10;密码: your-password&#10;{&quot;sauth_json&quot;:&quot;...&quot;}（可选）"
                :disabled="accounts.loading"
                allow-clear
              />
              <span class="security-hint">
                <SafetyOutlined class="hint-icon" />
                <span>Channel、账号和密码必填；sauth_json 可选，缺少时会用账号密码登录。密码按设置加密保存</span>
              </span>

              <div class="form-actions">
                <a-button type="primary" size="large" html-type="submit" block :loading="accounts.loading">
                  <template #icon><KeyOutlined /></template>
                  {{ accounts.loading ? '正在导入小号机账号...' : '一键导入' }}
                </a-button>
              </div>
            </div>
          </form>
        </a-tab-pane>
      </a-tabs>
    </div>
  </a-modal>
</template>

<style scoped>
.modal-body {
  padding-top: 6px;
}

.x19-mode {
  display: flex;
  width: 100%;
  margin-bottom: 14px;
}

.x19-mode :deep(.ant-radio-button-wrapper) {
  flex: 1;
  text-align: center;
}

.form-col {
  display: flex;
  flex-direction: column;
  gap: 12px;
  max-width: 480px;
}

.form-label {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--n-text-2);
  margin-bottom: -6px;
}

.input-icon {
  color: var(--n-text-3);
}

.form-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 4px;
}

.security-hint {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  font-size: 11.5px;
  color: var(--n-text-2);
  line-height: 1.5;
}

.hint-icon {
  color: var(--n-green);
  margin-top: 2px;
  flex-shrink: 0;
}
</style>
