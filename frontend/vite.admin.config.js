import path from 'path';
import { fileURLToPath } from 'url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// The Super Admin console: a separate bundle served by posd on
// admin.<BASE_DOMAIN> (STATIC_DIR/admin) under a strict CSP: static JS and
// CSS files only, no inline scripts or styles (OPERATIONS §7). Plain React
// and CSS, no MUI or Tailwind, nothing shared with the tenant app.
const behindCaddy = process.env.VITE_BEHIND_CADDY === '1';

export default defineConfig({
  root: path.resolve(__dirname, 'admin-console'),
  base: '/',
  plugins: [react()],
  server: {
    host: true,
    port: 5174,
    strictPort: true,
    allowedHosts: ['.localhost'],
    hmr: behindCaddy ? { protocol: 'wss', clientPort: Number(process.env.VITE_CADDY_PORT || 8443) } : undefined,
  },
  build: {
    outDir: path.resolve(__dirname, 'dist-admin'),
    emptyOutDir: true,
    assetsDir: 'assets',
    // Small assets must stay files: an inlined data: font or script would
    // need a looser CSP.
    assetsInlineLimit: 0,
    modulePreload: { polyfill: false },
  },
});
