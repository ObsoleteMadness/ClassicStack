import { defineConfig } from 'vite';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = fileURLToPath(new URL('.', import.meta.url));
const submodule = path.resolve(here, '../../../../third_party/classicstack-web');
const sibling = path.resolve(here, '../../../../../ClassicStack-web');
const webRoot = fs.existsSync(path.join(submodule, 'src')) ? submodule : sibling;

export default defineConfig({
  base: '/',
  plugins: [
    {
      name: 'classicstack-icons-static',
      configureServer(server) {
        const iconsDir = path.join(webRoot, 'icons');
        server.middlewares.use((req, res, next) => {
          if (!req.url?.startsWith('/icons/')) return next();
          const rel = decodeURIComponent(req.url.slice('/icons/'.length).split('?')[0] ?? '');
          if (!rel || rel.includes('..') || path.isAbsolute(rel)) {
            res.statusCode = 400;
            res.end('bad path');
            return;
          }
          const file = path.join(iconsDir, rel);
          if (!file.startsWith(iconsDir) || !fs.existsSync(file) || !fs.statSync(file).isFile()) {
            res.statusCode = 404;
            res.end('not found');
            return;
          }
          res.setHeader('Content-Type', 'image/png');
          fs.createReadStream(file).pipe(res);
        });
      },
      closeBundle() {
        const iconsDir = path.join(webRoot, 'icons');
        const outDir = path.join(here, '../spa/icons');
        if (!fs.existsSync(iconsDir)) return;
        fs.mkdirSync(outDir, { recursive: true });
        for (const name of fs.readdirSync(iconsDir)) {
          const src = path.join(iconsDir, name);
          if (fs.statSync(src).isFile()) fs.copyFileSync(src, path.join(outDir, name));
        }
      },
    },
  ],
  resolve: {
    alias: {
      'classicstack-web': path.join(webRoot, 'src'),
    },
  },
  build: {
    outDir: path.join(here, '../spa'),
    emptyOutDir: true,
    assetsDir: 'assets',
  },
});
