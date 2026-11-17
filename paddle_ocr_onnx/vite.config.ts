import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig(({ command }) => ({
  // Relative base so the built `dist/` can be hosted under any path
  // (e.g. embedded by core at /web) without rebuilding.
  base: command === 'build' ? './' : '/',
  plugins: [react(), tailwindcss()],
}))
