import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const backendTarget = process.env.VITE_BACKEND_TARGET || 'http://localhost:18080'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: backendTarget,
        changeOrigin: true
      },
      '/v1': {
        target: backendTarget,
        changeOrigin: true
      }
    }
  },
  build: {
    outDir: '../dist/web'
  }
})
