import { resolve } from "node:path";
import { defineConfig } from "vite";

export default defineConfig({
  build: {
    outDir: "dist",
    emptyOutDir: true,
    rollupOptions: {
      input: {
        settings: resolve(import.meta.dirname, "index.html"),
        setup: resolve(import.meta.dirname, "setup.html"),
      },
    },
  },
});
