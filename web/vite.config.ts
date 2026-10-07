import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    outDir: 'dist', // embedded into the Go binary by web/embed.go
    emptyOutDir: true,
    chunkSizeWarningLimit: 1600, // the ELK layout engine is ~1.4 MB and loads on demand
  },
  server: {
    // `npm run dev` talks to a Go server running on :8080 (`go run ./cmd/omini`).
    proxy: { '/api': 'http://localhost:8080' },
  },
})
