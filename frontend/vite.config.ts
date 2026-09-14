import path from 'node:path'
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    // Dashboard API calls use same-origin `/api/v1/*`; forward to the Go gateway.
    proxy: {
      '/api': {
        target: process.env.VITE_PROXY_TARGET || 'http://localhost:8088',
        changeOrigin: true,
      },
      '/admin': {
        target: process.env.VITE_PROXY_TARGET || 'http://localhost:8088',
        changeOrigin: true,
      },
      '/healthz': {
        target: process.env.VITE_PROXY_TARGET || 'http://localhost:8088',
        changeOrigin: true,
      },
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
  },
})
