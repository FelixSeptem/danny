import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The workbench is served by the Danny Go service from web/dist. All API and
// SSE calls are same-origin relative paths; there is no separate backend host.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
});
