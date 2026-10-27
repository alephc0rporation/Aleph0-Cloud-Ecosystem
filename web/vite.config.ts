import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  // One .env for the whole repo; only VITE_* variables reach the browser.
  envDir: '..',
  // The router plugin must run before the React plugin.
  plugins: [tanstackRouter({ target: 'react', autoCodeSplitting: true }), react(), tailwindcss()],
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') },
  },
  server: {
    port: 5173,
    // Keycloak only accepts redirects to :5173, so fail instead of moving to another port.
    strictPort: true,
    proxy: { '/v1': 'http://localhost:8080' },
  },
})
