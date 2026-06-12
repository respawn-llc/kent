# GUI Browser QA

The browser client is the primary manual QA path for Builder GUI work. It exercises the real React app against a Builder server.

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
