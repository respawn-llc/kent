import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";
import { protocolVersion, viteRuntimeAliases } from "./runtimeConfiguration";

const rpcEndpoint = new URL(process.env.VITE_APP_RPC_ENDPOINT ?? "ws://127.0.0.1:53082/rpc");
if (
  !["ws:", "wss:"].includes(rpcEndpoint.protocol) ||
  rpcEndpoint.username ||
  rpcEndpoint.password ||
  rpcEndpoint.hash
) {
  throw new Error("VITE_APP_RPC_ENDPOINT must be a ws:// or wss:// URL without credentials or fragment.");
}
const speechEndpoint = new URL(rpcEndpoint);
speechEndpoint.protocol = rpcEndpoint.protocol === "wss:" ? "https:" : "http:";

export default defineConfig({
  plugins: [tailwindcss(), react()],
  clearScreen: false,
  define: {
    __KENT_PROTOCOL_VERSION__: JSON.stringify(protocolVersion),
    __KENT_BROWSER_RPC_ENDPOINT__: JSON.stringify(rpcEndpoint.href),
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
    proxy: { "/speech": { target: speechEndpoint.origin } },
    watch: {
      ignored: ["**/src-tauri/**"],
    },
  },
});
