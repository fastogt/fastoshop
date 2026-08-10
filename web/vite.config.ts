import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  base: '/admin/',
  plugins: [react(), tailwindcss()],
  server: { proxy: { '/api': 'http://localhost:9097', '/uploads': 'http://localhost:9097' } },
})
