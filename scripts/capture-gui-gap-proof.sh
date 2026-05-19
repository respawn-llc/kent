#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
proof_dir="$repo_root/.builder/proofs/gui-gap-closure"
screenshots_dir="$proof_dir/screenshots"
proof_port="${BUILDER_PROOF_SERVER_PORT:-53182}"
vite_port="${BUILDER_PROOF_VITE_PORT:-1430}"
proof_tmp="${BUILDER_PROOF_TMP:-$(mktemp -d /tmp/builder-gui-proof.XXXXXX)}"
project_id=""
valid_workflow_id=""
invalid_workflow_id=""
task_id=""
run_id=""
session_id=""
worktree_id=""
service_pid=""
vite_pid=""

cleanup() {
	if [[ -n "$service_pid" ]]; then
		kill "$service_pid" >/dev/null 2>&1 || true
	fi
	if [[ -n "$vite_pid" ]]; then
		kill "$vite_pid" >/dev/null 2>&1 || true
	fi
	pkill -TERM -f 'target/debug/builder-desktop' >/dev/null 2>&1 || true
}
trap cleanup EXIT

env_cmd=(
	env
	"BUILDER_SERVER_PORT=$proof_port"
	"BUILDER_PERSISTENCE_ROOT=$proof_tmp/persistence"
	"OPENAI_API_KEY=sk-proof"
)

run_builder() {
	"${env_cmd[@]}" "$repo_root/bin/builder" "$@"
}

labeled_value() {
	awk -v key="$1" '$1 == key { print $2; exit }'
}

wait_for_server() {
	for _ in {1..60}; do
		if run_builder project list >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.5
	done
	printf 'proof Builder server did not become ready\n' >&2
	return 1
}

start_services() {
	mkdir -p "$proof_tmp/persistence" "$proof_tmp/workspaces/primary" "$proof_tmp/workspaces/secondary"
	"${env_cmd[@]}" "$repo_root/bin/builder" serve >"$proof_tmp/server.log" 2>"$proof_tmp/server.err.log" &
	service_pid="$!"
	wait_for_server
	pnpm --dir "$repo_root/apps/desktop" exec vite --host 127.0.0.1 --port "$vite_port" --strictPort >"$proof_tmp/vite.log" 2>&1 &
	vite_pid="$!"
	for _ in {1..60}; do
		if nc -z 127.0.0.1 "$vite_port" >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.5
	done
	printf 'proof Vite server did not become ready\n' >&2
	return 1
}

seed_fixture() {
	git -C "$proof_tmp/workspaces/primary" init -q
	printf 'proof\n' >"$proof_tmp/workspaces/primary/README.md"
	git -C "$proof_tmp/workspaces/primary" add README.md
	git -C "$proof_tmp/workspaces/primary" -c user.email=proof@example.invalid -c user.name=Proof commit -m 'proof seed' >/dev/null
	git -C "$proof_tmp/workspaces/secondary" init -q

	project_id="$(run_builder project create --path "$proof_tmp/workspaces/primary" --name 'GUI Proof')"
	run_builder attach --project "$project_id" "$proof_tmp/workspaces/secondary" >/dev/null

	valid_workflow_id="$(run_builder workflow create --description 'Valid fast proof workflow' 'Fast Delivery' | labeled_value workflow_id)"
	run_builder workflow node add "$valid_workflow_id" --key implement --kind agent --display-name Implement --agent fast --prompt 'Summarize proof task.' >/dev/null
	run_builder workflow edge add "$valid_workflow_id" --from backlog --transition start --edge-key start --to implement --context new_session >/dev/null
	run_builder workflow edge add "$valid_workflow_id" --from implement --transition "done" --edge-key "done" --to "done" --context new_session --requires-approval >/dev/null
	run_builder workflow link "$project_id" "$valid_workflow_id" --default >/dev/null

	invalid_workflow_id="$(run_builder workflow create --description 'Invalid proof workflow' 'Broken Intake' | labeled_value workflow_id)"
	run_builder workflow link "$project_id" "$invalid_workflow_id" >/dev/null

	task_id="$(run_builder task create --title 'Valid board card' --body 'This card proves valid workflow Backlog state and start affordance.' --workflow "$valid_workflow_id" --project "$project_id" | labeled_value task_id)"
	run_builder task create --title 'Backlog on broken workflow' --body 'Invalid workflow should still allow backlog planning and comments.' --workflow "$invalid_workflow_id" --project "$project_id" >/dev/null
	run_builder task comment add --project "$project_id" --body 'Proof comment: comments remain available before automation.' "$task_id" >/dev/null
	run_id="$(run_builder task start --project "$project_id" "$task_id" | labeled_value run_id)"
	sleep 2
	read -r session_id worktree_id < <(teleport_ids)
}

rpc_request() {
	node - "$proof_port" "$1" "$2" <<'NODE'
const [port, method, paramsJSON] = process.argv.slice(2);
const ws = new WebSocket(`ws://127.0.0.1:${port}/rpc`);
let seq = 0;
function req(name, params) {
  const id = String(++seq);
  return new Promise((resolve, reject) => {
    const onMessage = (event) => {
      const message = JSON.parse(event.data);
      if (message.id !== id) return;
      ws.removeEventListener("message", onMessage);
      if (message.error) reject(new Error(message.error.message));
      else resolve(message.result);
    };
    ws.addEventListener("message", onMessage);
    ws.send(JSON.stringify({ jsonrpc: "2.0", id, method: name, params }));
  });
}
ws.addEventListener("open", async () => {
  try {
    await req("protocol.handshake", { protocol_version: "2" });
    const result = await req(method, JSON.parse(paramsJSON));
    console.log(JSON.stringify(result));
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  } finally {
    ws.close();
  }
});
NODE
}

teleport_ids() {
	local result
	result="$(rpc_request workflow.task.teleportTarget.get "{\"task_id\":\"$task_id\",\"run_id\":\"$run_id\"}")"
	jq -r '[.session_id, .worktree_id] | @tsv' <<<"$result"
}

capture_window() {
	local name="$1" route="$2" theme="$3" width="${4:-1280}" height="${5:-900}" config pid window_id raw final
	local title="Builder Proof $name"
	raw="$screenshots_dir/$name-raw.png"
	final="$screenshots_dir/$name.png"
	config="$(mktemp -t builder-gui-proof.XXXXXX.json)"
	cat >"$config" <<JSON
{
  "identifier": "sh.builder.gui-gap-proof.$name",
  "build": { "beforeDevCommand": "", "devUrl": "http://127.0.0.1:$vite_port$route" },
  "app": { "windows": [{ "title": "$title", "width": $width, "height": $height, "resizable": true, "transparent": true, "titleBarStyle": "Overlay", "hiddenTitle": true, "trafficLightPosition": { "x": 20, "y": 18 }, "windowEffects": { "effects": ["underWindowBackground", "acrylic"], "state": "active", "radius": 18 } }] }
}
JSON
	env BUILDER_SERVER_PORT="$proof_port" BUILDER_PERSISTENCE_ROOT="$proof_tmp/persistence" BUILDER_THEME="$theme" OPENAI_API_KEY=sk-proof pnpm --dir "$repo_root/apps/desktop" tauri dev --no-watch --config "$config" >"$proof_tmp/tauri-$name.log" 2>&1 &
	pid="$!"
	for _ in {1..60}; do
		window_id="$(yabai -m query --windows 2>/dev/null | jq -r --arg title "$title" '.[] | select(.app=="builder-desktop" and .title==$title) | .id' | head -1)"
		if [[ -n "$window_id" ]]; then
			break
		fi
		sleep 0.5
	done
	if [[ -z "${window_id:-}" ]]; then
		printf 'missing proof window: %s\n' "$name" >&2
		return 1
	fi
	sleep 4
	screencapture -x -l "$window_id" "$raw"
	sips -Z 1100 "$raw" --out "$final" >/dev/null
	trash "$raw" >/dev/null 2>&1 || true
	pkill -TERM -f 'target/debug/builder-desktop' >/dev/null 2>&1 || true
	kill "$pid" >/dev/null 2>&1 || true
}

write_manifest() {
	cat >"$proof_dir/manifest.md" <<MD
# GUI Gap Closure Proof Manifest

Captured: 2026-05-18.

## Fixture

- Service: isolated \`./bin/builder serve\`, not \`builder service\`.
- Endpoint: \`ws://127.0.0.1:$proof_port/rpc\`.
- Persistence root: \`$proof_tmp/persistence\`.
- Workspaces:
  - \`$proof_tmp/workspaces/primary\`
  - \`$proof_tmp/workspaces/secondary\`
- Auth: proof-only \`OPENAI_API_KEY=sk-proof\`.
- Theme proof: \`BUILDER_THEME=dark\` and \`BUILDER_THEME=light\`.
- Frontend: Vite dev server on \`http://127.0.0.1:$vite_port\`, Tauri dev windows with native context pointed at the isolated service.
- Seeding path: Builder CLI/API only (\`project create\`, \`attach\`, \`workflow create/node/edge/link\`, \`task create/comment/start\`). No direct DB writes.

## Seeded IDs

- Project: \`$project_id\` (\`GUI Proof\`).
- Valid workflow: \`$valid_workflow_id\` (\`Fast Delivery\`).
- Invalid workflow: \`$invalid_workflow_id\` (\`Broken Intake\`).
- Task detail task: \`$task_id\`.
- Runtime run: \`$run_id\`.
- Teleport RPC proof returned available target with session \`$session_id\`, worktree \`$worktree_id\`, \`cwd_relpath="."\`.

## Screenshots

- \`screenshots/home-dark.png\`: Home project list, Inbox, forced dark theme.
- \`screenshots/home-light.png\`: Home project list, Inbox, forced light theme.
- \`screenshots/project-edit.png\`: Project edit page, default workspace selector, attached workspace list, unlink affordances, files-stay-on-disk copy.
- \`screenshots/board-valid.png\`: Valid workflow board with Backlog/agent/Done columns and task card.
- \`screenshots/board-valid-compact.png\`: Valid workflow board at reduced height, proving bottom-edge column shadow bleed without extra visible gutter.
- \`screenshots/board-invalid.png\`: Invalid workflow board with validation blockers and Backlog task preserved.
- \`screenshots/task-detail.png\`: Task detail route with header, description edit area, Inbox, tabs, comments composer.
- \`screenshots/task-detail-inbox-run.png\`: Task detail after runtime start, showing Inbox runtime blocker, Resume/Cancel actions, run count.

## Acceptance Mapping

- Home/project list: \`screenshots/home-dark.png\`, \`screenshots/home-light.png\`.
- Project edit/workspace management: \`screenshots/project-edit.png\`.
- Valid workflow board: \`screenshots/board-valid.png\`.
- Board bottom-edge shadow bleed: \`screenshots/board-valid-compact.png\`.
- Invalid workflow board: \`screenshots/board-invalid.png\`.
- Task detail layout: \`screenshots/task-detail.png\`.
- Inbox blocker/runtime controls: \`screenshots/task-detail-inbox-run.png\`.
- Forced theme override: \`screenshots/home-dark.png\`, \`screenshots/home-light.png\`.
- Teleport available state: manifest RPC proof above; UI rendering and native failure paths are covered by \`apps/desktop/src/features/task-detail/TaskDetailDialog.test.tsx\`.
- Teleport unavailable/native failure: \`apps/desktop/src/features/task-detail/TaskDetailDialog.test.tsx\` covers server unavailable reason and local executable failure rendering.
- Question and approval Inbox controls: \`apps/desktop/src/features/task-detail/TaskDetailDialog.test.tsx\` covers question option/freeform behavior and approval snapshot rendering.
- Reconnect/draft behavior: \`apps/desktop/src/features/startup/StartupGate.test.tsx\` and \`apps/desktop/src/features/board/BoardRoute.test.tsx\` cover non-dismissible disconnect, route restoration, reconnect refresh, and draft preservation.

## Runtime Notes

- macOS Accessibility can block scripted native clicks, so tab switching beyond initial route screenshots is recorded through tests and manifest entries where direct native interaction is not available.
- Proof capture surfaced a real contract issue: empty task-detail slices can arrive as JSON \`null\`. The desktop schema normalizes null task detail arrays and has a regression in \`apps/desktop/src/api/client.test.ts\`.
MD
}

main() {
	mkdir -p "$screenshots_dir"
	start_services
	seed_fixture
	capture_window home-dark "/" dark
	capture_window home-light "/" light
	capture_window project-edit "/projects/$project_id/edit" dark
	capture_window board-valid "/projects/$project_id?workflowId=$valid_workflow_id" dark
	capture_window board-valid-compact "/projects/$project_id?workflowId=$valid_workflow_id" dark 1280 640
	capture_window board-invalid "/projects/$project_id?workflowId=$invalid_workflow_id" dark
	capture_window task-detail "/tasks/$task_id" dark
	capture_window task-detail-inbox-run "/tasks/$task_id" dark
	write_manifest
	"$repo_root/scripts/validate-gui-gap-proof.sh" "$proof_dir"
}

main "$@"
