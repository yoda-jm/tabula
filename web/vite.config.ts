import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

// The tabula binary serves the API; in development Vite serves the GUI and
// forwards /api (including the /api/events stream) to it. Point TABULA_API
// elsewhere if tabula listens on another port.
const api = process.env.TABULA_API ?? "http://localhost:8080";

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/api": { target: api, changeOrigin: true },
    },
  },
  preview: {
    proxy: {
      "/api": { target: api, changeOrigin: true },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
  },
});
