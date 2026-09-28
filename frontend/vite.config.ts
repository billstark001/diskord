import { defineConfig } from "vite";
import preact from "@preact/preset-vite";
import { vanillaExtractPlugin } from "@vanilla-extract/vite-plugin";
import { fileURLToPath } from "node:url";

export default defineConfig({
  base: "/ui/",
  plugins: [preact(), vanillaExtractPlugin()],
  build: {
    outDir: fileURLToPath(new URL("../internal/ui/webdist", import.meta.url)),
    emptyOutDir: true,
    assetsDir: "assets",
    rolldownOptions: {
      output: {
        entryFileNames: "assets/app.js",
        chunkFileNames: "assets/[name].js",
        assetFileNames: "assets/[name][extname]",
      },
    },
  },
});
