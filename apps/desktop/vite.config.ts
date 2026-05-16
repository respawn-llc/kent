import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  clearScreen: false,
  resolve: {
    alias: {
      "@builder/desktop-native-bridge": fileURLToPath(
        new URL("./packages/native-bridge/src/index.ts", import.meta.url),
      ),
    },
  },
  server: {
    host: "127.0.0.1",
    port: 1420,
    strictPort: true,
    watch: {
      ignored: ["**/src-tauri/**"],
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./test/setup.ts"],
  },
});
