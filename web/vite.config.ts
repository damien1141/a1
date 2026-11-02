import { defineConfig } from 'vite';
import preact from '@preact/preset-vite';
import { resolve } from 'path';

export default defineConfig({
  plugins: [preact()],
  resolve: {
    alias: {
      react: 'preact/compat',
      'react-dom': 'preact/compat',
      // lucide-react and lucide-preact share the same component shapes;
      // aliasing keeps the (more common) `lucide-react` import name working
      // without pulling in the React deps.
      'lucide-react': 'lucide-preact',
    },
  },
  build: {
    outDir: '../internal/webserver/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      // Match the Go server's DefaultPort (9001). An earlier value of 7331
      // made `bun run dev` 502 every /api call unless the dev also ran
      // `a1 --web --port 7331` to match — easy to miss.
      '/api': 'http://127.0.0.1:9001',
    },
  },
});
