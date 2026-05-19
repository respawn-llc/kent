# GUI Browser QA

The browser client is the primary manual QA path for Builder GUI work. It exercises the real React app against a Builder server and works with `agent-browser`, avoiding macOS window automation.

Run it from the repo root:

```sh
pnpm --dir apps/desktop dev:browser
```

Open:

```text
http://127.0.0.1:1420/
```

By default the browser client connects to the existing local Builder server:

```text
ws://127.0.0.1:53082/rpc
```

For a one-off endpoint override, pass `builderRpcEndpoint`:

```text
http://127.0.0.1:1420/?builderRpcEndpoint=ws%3A%2F%2F127.0.0.1%3A53082%2Frpc
```

For a dev-server-wide override, set:

```sh
VITE_BUILDER_RPC_ENDPOINT=ws://127.0.0.1:53082/rpc pnpm --dir apps/desktop dev:browser
```

Capture proof screenshots:

```sh
./scripts/capture-gui-browser-proof.sh
```

The proof script starts only the Vite browser client and an agent-browser-managed Chromium session. It does not start `builder serve`; the target Builder server must already be running. Override the endpoint with `BUILDER_GUI_BROWSER_RPC_ENDPOINT`.

The script writes an empty temporary `agent-browser` config and passes it with `--config` on every browser command. This intentionally ignores user-level CDP settings, so proof capture launches an isolated managed Chromium session instead of attaching to a stale or user-owned browser.

Default output:

```text
.builder/proofs/gui-browser/
```
