import path from 'path';
import { fileURLToPath } from 'url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  base: '/',
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') },
  },
  plugins: [react(), tailwindcss()],
  server: {
    host: true,
    // 5173 is the shop app (frontend/).
    port: 5174,
    strictPort: true,
  },
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
  },
});
