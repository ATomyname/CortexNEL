/// <reference types="vite/client" />

// Vue SFC 在 .ts 中的类型声明。
declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<{}, {}, any>
  export default component
}
