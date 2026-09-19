import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The generated media folder is served as-is, so the page can fetch /clip-h264-aac.mp4.
export default defineConfig({
  plugins: [react()],
  publicDir: '../media',
  build: { outDir: '../out/dist', emptyOutDir: true, copyPublicDir: false },
});
