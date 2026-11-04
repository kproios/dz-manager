import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Built into ../internal/web/dist and embedded in the Go binary.
export default defineConfig({
  plugins: [react()],
  build: { outDir: '../internal/web/dist', emptyOutDir: false, assetsDir: 'assets', sourcemap: false },
  server: { proxy: { '/api': 'http://127.0.0.1:8080', '/auth': 'http://127.0.0.1:8080', '/tiles': 'http://127.0.0.1:8080' } },
})
