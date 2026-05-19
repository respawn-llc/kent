#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
output="${1:-/tmp/builder-native-dialog-widget-proof.png}"
review_output="${2:-${output%.*}-review.png}"
url="http://127.0.0.1:1420/native-dialog/project-create?name=kent-marketing&key=KENTMARK&workspaceRoot=%2FUsers%2Fnek%2FDeveloper%2Fkent-marketing"
config="$(mktemp -t builder-native-dialog-proof.XXXXXX.json)"

cleanup() {
	local pid="${proof_pid:-}"
	if [ -n "$pid" ]; then
		pkill -TERM -P "$pid" >/dev/null 2>&1 || true
		kill -TERM "$pid" >/dev/null 2>&1 || true
	fi
	if command -v trash >/dev/null 2>&1; then
		trash "$config" >/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT

cat >"$config" <<JSON
{
  "identifier": "sh.kent.native-dialog-proof",
  "build": {
    "beforeDevCommand": "",
    "devUrl": "$url"
  },
  "app": {
    "windows": [
      {
        "title": "Create project",
        "width": 640,
        "height": 440,
        "resizable": false,
        "maximizable": false,
        "transparent": true,
        "titleBarStyle": "Overlay",
        "hiddenTitle": true,
        "trafficLightPosition": { "x": 20, "y": 18 },
        "windowEffects": {
          "effects": ["underWindowBackground", "acrylic"],
          "state": "active",
          "radius": 18
        }
      }
    ]
  }
}
JSON

cd "$repo_root"

pnpm --dir apps/desktop tauri dev --no-watch --config "$config" >/tmp/builder-native-dialog-proof.log 2>&1 &
proof_pid="$!"

sleep 6
screencapture -x "$output"

screenshot_width="$(sips -g pixelWidth "$output" | awk '/pixelWidth/ { print $2 }')"
screenshot_height="$(sips -g pixelHeight "$output" | awk '/pixelHeight/ { print $2 }')"
crop_width="$((screenshot_width < 1400 ? screenshot_width : 1400))"
crop_height="$((screenshot_height < 1100 ? screenshot_height : 1100))"
crop_x="$(((screenshot_width - crop_width) / 2))"
crop_y="$(((screenshot_height - crop_height) / 3))"
sips --cropToHeightWidth "$crop_height" "$crop_width" --cropOffset "$crop_y" "$crop_x" "$output" --out "$review_output" >/dev/null
sips -Z 900 "$review_output" --out "$review_output" >/dev/null

printf '%s\n' "$output"
printf '%s\n' "$review_output"
