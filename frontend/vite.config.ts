import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'

// El frontend sólo habla con el api-gateway (docs/ARCHITECTURE.md). En
// desarrollo, Vite reenvía /api al gateway para que el navegador vea un único
// origen y no haga falta CORS. En docker compose lo hace nginx (nginx.conf).
const gateway = process.env.API_GATEWAY_URL ?? 'http://localhost:8080'

export default defineConfig({
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, 'src'),
    },
  },
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: gateway, changeOrigin: true },
    },
  },
})
