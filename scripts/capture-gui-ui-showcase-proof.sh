#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
proof_dir="${BUILDER_SHOWCASE_PROOF_DIR:-$repo_root/.builder/proofs/gui-ui-showcase}"
vite_port="${BUILDER_SHOWCASE_VITE_PORT:-1421}"
browser_session="${BUILDER_SHOWCASE_BROWSER_SESSION:-builder-gui-showcase}"
tmp_root="${BUILDER_SHOWCASE_TMP:-$(mktemp -d /tmp/builder-gui-showcase.XXXXXX)}"
vite_pid=""

cleanup() {
	agent-browser --session-name "$browser_session" close >/dev/null 2>&1 || true
	if [[ -n "$vite_pid" ]]; then
		kill "$vite_pid" >/dev/null 2>&1 || true
	fi
	if command -v trash >/dev/null 2>&1; then
		trash "$tmp_root" >/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT

wait_for_port() {
	local port="$1"
	for _ in {1..80}; do
		if nc -z 127.0.0.1 "$port" >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.25
	done
	printf 'port did not become ready: %s\n' "$port" >&2
	return 1
}

configured_cdp_url() {
	if [[ -n "${BUILDER_SHOWCASE_CDP_URL:-}" ]]; then
		printf '%s\n' "$BUILDER_SHOWCASE_CDP_URL"
		return
	fi
	if [[ -n "${AGENT_BROWSER_CDP:-}" ]]; then
		printf '%s\n' "$AGENT_BROWSER_CDP"
		return
	fi
	node -e '
const fs = require("fs");
const path = `${process.env.HOME}/.agent-browser/config.json`;
try {
  const config = JSON.parse(fs.readFileSync(path, "utf8"));
  if (typeof config.cdp === "string") process.stdout.write(config.cdp);
} catch {
}
'
}

cdp_port_from_url() {
	node -e '
const value = process.argv[1];
try {
  const parsed = new URL(value);
  process.stdout.write(parsed.port || "9222");
} catch {
  process.exit(1);
}
' "$1"
}

mkdir -p "$proof_dir"

if ! command -v agent-browser >/dev/null 2>&1; then
	printf 'agent-browser is required.\n' >&2
	exit 1
fi

cdp_url="$(configured_cdp_url)"
if [[ -n "${BUILDER_SHOWCASE_CDP_PORT:-}" ]]; then
	cdp_port="$BUILDER_SHOWCASE_CDP_PORT"
elif [[ -n "$cdp_url" ]]; then
	cdp_port="$(cdp_port_from_url "$cdp_url")"
else
	cdp_port="9222"
	cdp_url="ws://127.0.0.1:9222/devtools/browser"
fi

if ! nc -z 127.0.0.1 "$cdp_port" >/dev/null 2>&1; then
	printf 'existing browser CDP endpoint is required: %s\n' "$cdp_url" >&2
	printf 'Start or attach Comet with remote debugging, then rerun. This script never launches a browser.\n' >&2
	exit 1
fi

pnpm --dir "$repo_root/apps/desktop" exec vite --host 127.0.0.1 --port "$vite_port" --strictPort >"$tmp_root/vite.log" 2>&1 &
vite_pid="$!"
wait_for_port "$vite_port"

agent-browser --session-name "$browser_session" connect "$cdp_port"
agent-browser --session-name "$browser_session" open "http://127.0.0.1:$vite_port/dev-showcase.html"
agent-browser --session-name "$browser_session" wait --text "Builder UI Showcase"
agent-browser --session-name "$browser_session" set viewport 1600 1200
agent-browser --session-name "$browser_session" screenshot --full --screenshot-dir "$proof_dir"
agent-browser --session-name "$browser_session" click '[data-testid="dev-showcase-sonner-success"]'
agent-browser --session-name "$browser_session" wait '[data-sonner-toast]'
agent-browser --session-name "$browser_session" screenshot --screenshot-dir "$proof_dir"

cat >"$proof_dir/manifest.md" <<EOF
# GUI UI Showcase Proof

- Page: http://127.0.0.1:$vite_port/dev-showcase.html
- Browser: existing CDP endpoint on port $cdp_port; script did not launch a browser.
- Main capture: full-page Builder UI Showcase after loading static mock data.
- Toast capture: clicked \`Sonner Toasts\` success trigger and verified \`[data-sonner-toast]\` before screenshot.
EOF

printf 'gui ui showcase proof captured: %s\n' "$proof_dir"
