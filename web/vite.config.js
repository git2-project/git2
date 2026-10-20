import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In dev the browser talks only to Vite; /api is proxied to the Go binary so
// the app stays same-origin, as it is behind the gateway in production.
const apiTarget = process.env.API_URL ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react()],
  server: {
    host: true,
    port: Number(process.env.WEB_PORT ?? 5173),
    proxy: { '/api': apiTarget },
  },
})
