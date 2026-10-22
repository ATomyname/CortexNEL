import { createApp } from 'vue'
import { createPinia } from 'pinia'
import Antd from 'ant-design-vue'
import App from './App.vue'
import { router } from './router'
import { registerEventBridge } from './lib/bus'
import 'ant-design-vue/dist/reset.css'
import './styles/tokens.css'

const app = createApp(App)

app.use(createPinia())
app.use(router)
app.use(Antd)

// 事件桥在挂载前注册，保证不漏掉早期推送的事件。
registerEventBridge()

app.mount('#app')
