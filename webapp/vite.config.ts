import { defineConfig } from 'vite'
import solid from 'vite-plugin-solid'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [solid()],
  resolve: {
    alias: {
      // Matches longhouse's webapp convention (and tsconfig.app.json's
      // "~/*" path mapping): a stable root-relative import so deep
      // relative paths (../../gen/...) aren't needed from pages/components.
      '~': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    // `npm run dev` proxies the CSIL-RPC mount straight through to
    // firepit-api's dev port (FIREPIT_PORT default 8080, docker-compose.yaml)
    // so the same relative `/csil/v1/rpc` src/lib/httpTransport.ts uses in
    // production works unchanged in dev — no `/api` prefix indirection
    // (unlike longhouse's webapp, which proxies `/api`; firepit-api mounts
    // this path directly, per CLAUDE.md's architecture diagram).
    proxy: {
      '/csil': { target: 'http://localhost:8080', changeOrigin: false },
      '/healthz': { target: 'http://localhost:8080', changeOrigin: false },
    },
  },
})
