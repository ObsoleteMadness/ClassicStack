import { defineConfig } from 'vite';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = fileURLToPath(new URL('.', import.meta.url));
const submodule = path.resolve(here, '../../../../third_party/classicstack-web');
const sibling = path.resolve(here, '../../../../../ClassicStack-web');
const envWeb = process.env.WEB_DIR ? path.resolve(process.env.WEB_DIR) : '';
/** WEB_DIR, then the submodule pin, then a sibling ClassicStack-web checkout. */
const webRoot = [envWeb, submodule, sibling].find((dir) => dir && fs.existsSync(path.join(dir, 'src'))) || submodule;

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
          const ext = path.extname(file).toLowerCase();
          const type =
            ext === '.gif' ? 'image/gif' : ext === '.svg' ? 'image/svg+xml' : 'image/png';
          res.setHeader('Content-Type', type);
          fs.createReadStream(file).pipe(res);
        });
      },
      closeBundle() {
        const iconsDir = path.join(webRoot, 'icons');
        const outDir = path.join(here, '../spa/icons');
        if (!fs.existsSync(iconsDir)) return;
        const copyTree = (srcDir: string, destDir: string): void => {
          fs.mkdirSync(destDir, { recursive: true });
          for (const name of fs.readdirSync(srcDir)) {
            if (name === '.DS_Store') continue;
            const src = path.join(srcDir, name);
            const dest = path.join(destDir, name);
            const st = fs.statSync(src);
            if (st.isDirectory()) copyTree(src, dest);
            else if (st.isFile()) fs.copyFileSync(src, dest);
          }
        };
        copyTree(iconsDir, outDir);
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
