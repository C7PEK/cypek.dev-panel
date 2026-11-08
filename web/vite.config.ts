import path from 'node:path'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  server: {
    // Bind IPv4 explicitly: "localhost" may resolve to ::1 only, which breaks the Go dev proxy.
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    // Only used when opening Vite directly; `make dev` serves through the Go backend instead.
    proxy: { '/api': 'http://127.0.0.1:8420' },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
