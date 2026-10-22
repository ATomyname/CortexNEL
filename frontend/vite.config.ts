import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Wails 通过 window 注入 Go 绑定与运行时（wailsjs 由 wails 生成，勿手改）。
export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
