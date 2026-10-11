import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      '/auth':     'http://localhost:4400',
      '/cl':       'http://localhost:4400',
      '/user':     'http://localhost:4400',
      '/git':      'http://localhost:4400',
      '/health':   'http://localhost:4400',
      '/internal': 'http://localhost:4400',
    },
  },
})
