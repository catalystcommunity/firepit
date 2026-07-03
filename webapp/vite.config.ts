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
})
