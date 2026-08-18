import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    // Fail rather than quietly moving to 5174. The backend allows exactly this
    // origin (CORS_ALLOWED_ORIGINS), so a shifted port would surface later as a
    // puzzling CORS error instead of a clear "port already in use" here.
    strictPort: true,
  },
})
