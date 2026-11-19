import fs from 'fs';
import { createRequire } from 'module';
import path from 'path';
import { fileURLToPath } from 'url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);

/**
 * Preload the Latin Inter font in the built index.html. The file name is
 * hashed, so it is looked up in the bundle; without this the browser only
 * finds the font after the CSS has downloaded.
 */
function preloadInter() {
  return {
    name: 'preload-inter-latin',
    apply: 'build',
    transformIndexHtml: {
      order: 'post',
      handler(_html, ctx) {
        const file = Object.keys(ctx.bundle ?? {}).find((f) => /inter-latin-wght-normal-[\w-]+\.woff2$/.test(f));
        if (!file) return [];
        return [
          {
            tag: 'link',
            attrs: { rel: 'preload', href: `/${file}`, as: 'font', type: 'font/woff2', crossorigin: '' },
            injectTo: 'head',
          },
        ];
      },
    },
  };
}

/**
 * Dev server only: rewrite `import { Menu, X } from 'lucide-react'` into one
 * import per icon file. Otherwise Vite pre-bundles the whole icon set (about
 * 1 MB, 1,600+ icons) and every page load in dev has to download and parse
 * it. Production builds tree-shake the icons already, so this is not applied
 * there.
 */
function lucideDevIcons() {
  let icons;
  // Map every exported icon name (and alias) to its file, read from the package entry.
  const iconFiles = () => {
    if (icons) return icons;
    const entry = require.resolve('lucide-react/dist/esm/lucide-react.mjs');
    icons = new Map();
    const src = fs.readFileSync(entry, 'utf8');
    for (const [, names, file] of src.matchAll(/export \{([^}]+)\} from '\.\/icons\/([\w-]+)\.mjs'/g)) {
      for (const part of names.split(',')) icons.set(part.trim().replace(/^default as /, ''), file);
    }
    return icons;
  };
  return {
    name: 'lucide-dev-icons',
    apply: 'serve',
    enforce: 'pre',
    config: () => ({ optimizeDeps: { exclude: ['lucide-react'] } }),
    transform(code, id) {
      if (id.includes('node_modules') || !/\.[jt]sx?$/.test(id) || !code.includes('lucide-react')) return null;
      const map = iconFiles();
      const out = code.replace(/import\s*\{([^}]+)\}\s*from\s*['"]lucide-react['"];?/g, (whole, names) => {
        const lines = [];
        const rest = [];
        for (const spec of names.split(',').map((s) => s.trim()).filter(Boolean)) {
          const [imported, local = imported] = spec.split(/\s+as\s+/);
          const file = map.get(imported);
          if (file) lines.push(`import ${local} from 'lucide-react/dist/esm/icons/${file}.mjs';`);
          else rest.push(spec);
        }
        if (rest.length) lines.push(`import { ${rest.join(', ')} } from 'lucide-react';`);
        // Keep the line count so dev line numbers still match the source.
        return lines.join(' ') + '\n'.repeat((whole.match(/\n/g) || []).length);
      });
      return out === code ? null : { code: out, map: null };
    },
  };
}

export default defineConfig({
  base: '/',
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') },
  },
  plugins: [lucideDevIcons(), react(), tailwindcss(), preloadInter()],
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
