import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig(({ command }) => ({
  // Built output is served by server.py at /web (see ../server.py), so
  // asset URLs need that prefix; the dev server (npm run dev) is served at
  // its own origin's root instead.
  base: command === 'build' ? '/web/' : '/',
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
}))
