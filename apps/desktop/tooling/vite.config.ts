import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";
import { protocolVersion, viteRuntimeAliases } from "./runtimeConfiguration";

export default defineConfig({
  plugins: [tailwindcss(), react()],
  clearScreen: false,
  define: {
    __KENT_PROTOCOL_VERSION__: JSON.stringify(protocolVersion),
  },
  build: {
    // Desktop bundle loads from local disk; keep warning for real growth without treating current MVP size as web risk.
    chunkSizeWarningLimit: 2_048,
    manifest: true,
    // Never ship JS source maps in release artifacts (they expose original
    // source). Matches Vite's default; pinned explicitly to prevent regressions.
    sourcemap: false,
  },
  resolve: { alias: viteRuntimeAliases },
  server: {
    host: "127.0.0.1",
    port: 1420,
    strictPort: true,
    watch: {
      ignored: ["**/src-tauri/**"],
    },
  },
});
