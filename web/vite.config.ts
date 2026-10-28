import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

const target = 'http://127.0.0.1:8686'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    // dist/.gitkeep must survive; scripts/postbuild.mjs removes stale assets.
    emptyOutDir: false,
    assetsDir: 'assets',
    sourcemap: false,
    target: 'es2022',
  },
  server: {
    proxy: { '/api': target, '/healthz': target },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    css: false,
  },
})
