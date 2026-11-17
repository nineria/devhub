import { defineConfig, lazyPlugins } from "vite-plus";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: lazyPlugins(() => [react(), tailwindcss()]),
  build: { outDir: "dist", emptyOutDir: false },
  server: { proxy: { "/api": "http://127.0.0.1:8090" } },
});
