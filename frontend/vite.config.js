import path from 'path';
import { fileURLToPath } from 'url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// Development: open the app through Caddy (https://<shop>.localhost:8443)
// so the page and /api share one origin, as in production. Caddy sends /api
// to posd and everything else to this dev server when the stack runs with
// FRONTEND_UPSTREAM=host.docker.internal:5173. The HMR socket then goes
// through Caddy's port as well.
const behindCaddy = process.env.VITE_BEHIND_CADDY === '1';

export default defineConfig({
  base: '/', // path-based routes (/sales, /reset-password) need absolute asset URLs
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  plugins: [react(), tailwindcss()],
  server: {
    host: true,
    port: 5173,
    strictPort: true,
    allowedHosts: ['.localhost'],
    hmr: behindCaddy ? { protocol: 'wss', clientPort: Number(process.env.VITE_CADDY_PORT || 8443) } : undefined,
  },
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
    manifest: true,
  },
});
