import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
        headers: { Origin: 'http://127.0.0.1:8080' },
        bypass(request) {
          // The browser is same-origin with Vite, but Go sees the proxied
          // request. Rewrite Origin to the upstream origin so strict CSRF
          // validation remains enabled during local development.
          request.headers.origin = 'http://127.0.0.1:8080'
        },
      },
    },
  },
  preview: {
    host: '127.0.0.1',
    port: 4173,
  },
})
