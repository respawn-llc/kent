#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
proof_dir="${BUILDER_GUI_BROWSER_PROOF_DIR:-$repo_root/.builder/proofs/gui-browser}"
vite_port="${BUILDER_GUI_BROWSER_VITE_PORT:-1422}"
browser_session="${BUILDER_GUI_BROWSER_SESSION:-builder-gui-browser}"
rpc_endpoint="${BUILDER_GUI_BROWSER_RPC_ENDPOINT:-${VITE_BUILDER_RPC_ENDPOINT:-ws://127.0.0.1:53082/rpc}}"
skip_server_check="${BUILDER_GUI_BROWSER_SKIP_SERVER_CHECK:-}"
tmp_base="${BUILDER_GUI_BROWSER_TMP:-/tmp}"
mkdir -p "$tmp_base"
tmp_root="$(mktemp -d "$tmp_base/builder-gui-browser.XXXXXX")"
agent_browser_config="$tmp_root/agent-browser.json"
vite_pid=""

cleanup() {
	if command -v agent-browser >/dev/null 2>&1; then
		agent-browser --config "$agent_browser_config" --session-name "$browser_session" close >/dev/null 2>&1 || true
	fi
	if [[ -n "$vite_pid" ]]; then
		pkill -P "$vite_pid" >/dev/null 2>&1 || true
		kill "$vite_pid" >/dev/null 2>&1 || true
	fi
	if command -v trash >/dev/null 2>&1; then
		trash "$tmp_root" >/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT

run_agent_browser() {
	agent-browser --config "$agent_browser_config" --session-name "$browser_session" "$@"
}

wait_for_port() {
	local port="$1"
	for _ in {1..80}; do
		if [[ -n "$vite_pid" ]] && ! kill -0 "$vite_pid" >/dev/null 2>&1; then
			printf 'Vite exited before port became ready. Log:\n' >&2
			sed -n '1,120p' "$tmp_root/vite.log" >&2 || true
			return 1
		fi
		if nc -z 127.0.0.1 "$port" >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.25
	done
	printf 'port did not become ready: %s\n' "$port" >&2
	return 1
}

url_encode() {
	node -e 'process.stdout.write(encodeURIComponent(process.argv[1]));' "$1"
}

server_health_url() {
	node -e '
const raw = process.argv[1];
try {
  const parsed = new URL(raw);
  if (parsed.protocol !== "ws:" && parsed.protocol !== "wss:") process.exit(2);
  parsed.protocol = parsed.protocol === "wss:" ? "https:" : "http:";
  parsed.pathname = "/healthz";
  parsed.search = "";
  parsed.hash = "";
  process.stdout.write(parsed.toString());
} catch {
  process.exit(1);
}
' "$1"
}

mkdir -p "$proof_dir"
printf '{}\n' >"$agent_browser_config"

if ! command -v agent-browser >/dev/null 2>&1; then
	printf 'agent-browser is required.\n' >&2
	exit 1
fi

if [[ -z "$skip_server_check" ]]; then
	health_url="$(server_health_url "$rpc_endpoint")"
	if ! curl --fail --silent --show-error --max-time 5 "$health_url" >/dev/null; then
		printf 'Builder server is not reachable at %s (derived from %s).\n' "$health_url" "$rpc_endpoint" >&2
		printf 'Start or select a Builder server, or set BUILDER_GUI_BROWSER_RPC_ENDPOINT for isolation.\n' >&2
		exit 1
	fi
fi

VITE_BUILDER_RPC_ENDPOINT="$rpc_endpoint" pnpm --dir "$repo_root/apps/desktop" exec vite --host 127.0.0.1 --port "$vite_port" --strictPort >"$tmp_root/vite.log" 2>&1 &
vite_pid="$!"
wait_for_port "$vite_port"

encoded_rpc_endpoint="$(url_encode "$rpc_endpoint")"
page_url="http://127.0.0.1:$vite_port/?builderRpcEndpoint=$encoded_rpc_endpoint"

run_agent_browser open "$page_url"
run_agent_browser wait --text "Projects"
run_agent_browser set viewport 1600 1200
run_agent_browser screenshot --full --screenshot-dir "$proof_dir"
run_agent_browser snapshot -i >"$proof_dir/snapshot.txt"

cat >"$proof_dir/manifest.md" <<EOF
# GUI Browser Client Proof

- Page: $page_url
- RPC endpoint: $rpc_endpoint
- Browser: agent-browser-managed Chromium session \`$browser_session\`.
- Server: existing Builder server; this script does not start \`builder serve\`.
- Main capture: full-page browser GUI after the real app reached the Projects route.
- Accessibility snapshot: \`snapshot.txt\`.
EOF

printf 'gui browser client proof captured: %s\n' "$proof_dir"
