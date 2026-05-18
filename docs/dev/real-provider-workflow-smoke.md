# Real Provider Workflow Smoke

Use this only after explicit approval for provider spend. Keep it isolated from the developer's normal Builder persistence root and workspace.

This smoke covers the Slice 8 execution path only:

- `backlog` start node
- one agent node with `new_session`
- `done` terminal node
- real provider structured output
- no code edits by the workflow

It intentionally does not cover later pipeline features such as `continue_session`, `compact_and_continue_session`, fanout, joins, approvals, questions, or manual resume.

## Isolation Rules

- Use a temp `HOME`.
- Use a temp `BUILDER_PERSISTENCE_ROOT`.
- Copy the existing auth file into the temp persistence root if using saved auth: `cp "$HOME/.builder/auth.json" "$PERSIST/auth.json" && chmod 600 "$PERSIST/auth.json"`.
- Clone the repository into a temp workspace and bind the project to that clone.
- Set a unique `BUILDER_SERVER_PORT`.
- Configure the smoke subagent in the temp home config, not the real home config.
- Disable edit tools for the smoke role with `[subagents.workflow-smoke.tools] patch = false` and `edit = false`.

## Command Skeleton

```bash
set -euo pipefail

BIN="/absolute/path/to/bin/builder"
SRC="/absolute/path/to/builder/repo"
ROOT="$(mktemp -d /tmp/builder-real-workflow-smoke.XXXXXX)"
PERSIST="$ROOT/persist"
QA_HOME="$ROOT/home"
WS="$ROOT/workspace"
PORT="$(python3 - <<'PY'
import socket
s=socket.socket(); s.bind(('127.0.0.1',0)); print(s.getsockname()[1]); s.close()
PY
)"

mkdir -p "$PERSIST" "$QA_HOME/.builder"
cp "$HOME/.builder/auth.json" "$PERSIST/auth.json"
chmod 600 "$PERSIST/auth.json"
git clone --quiet "$SRC" "$WS"

cat > "$QA_HOME/.builder/config.toml" <<'TOML'
model = "gpt-5.4-mini"
thinking_level = "low"

[workflow]
completion_mode = "auto"
concurrency = 1
max_final_answer_violations = 2
max_invalid_completion_attempts = 3

[subagents.workflow-smoke]
description = "Read-only workflow smoke agent for inspecting the Builder codebase and producing a concise markdown plan. Never edit files."
model = "gpt-5.4-mini"
thinking_level = "low"

[subagents.workflow-smoke.tools]
shell = true
patch = false
edit = false
ask_question = false
TOML

export HOME="$QA_HOME"
export BUILDER_PERSISTENCE_ROOT="$PERSIST"
export BUILDER_SERVER_HOST="127.0.0.1"
export BUILDER_SERVER_PORT="$PORT"
export BUILDER_WORKFLOW_COMPLETION_MODE="auto"
export BUILDER_WORKFLOW_CONCURRENCY="1"
export BUILDER_MODEL="gpt-5.4-mini"
export BUILDER_THINKING_LEVEL="low"

"$BIN" serve >"$ROOT/server.log" 2>&1 &
SERVER_PID=$!
trap 'kill "$SERVER_PID" >/dev/null 2>&1 || true; wait "$SERVER_PID" >/dev/null 2>&1 || true' EXIT

"$BIN" project create --path "$WS" --name "Workflow Real Smoke"
"$BIN" workflow create --description "Real provider single-agent workflow smoke: inspect codebase and produce markdown plan, no edits." "Real LLM Smoke"
```

After creating the project/workflow, add one agent node with role `workflow-smoke`, connect:

- `backlog --start/new_session--> plan`
- `plan --done/new_session--> done`

Then link workflow as default, validate execution mode, create task, start task, and poll `builder task show` until `done true`.

## Last Known Smoke Result

Date: 2026-05-15

Result: passed after fixing native structured-output schema shape.

Found bug: OpenAI strict `json_schema` rejected workflow completion schema because strict mode requires every property to appear in `required`. Fixed by requiring `transition_id`, `commentary`, and every node output field in `workflowruntime.CompletionJSONSchema`.

Passing isolated run:

- root: `/tmp/builder-real-workflow-smoke.HqcO5I`
- project: `project-8335af34-9d6a-4ba4-8c83-6f39971a00a6`
- workflow: `workflow-21060fdc-fd08-43a5-9f2e-5ab87b660b70`
- task: `WOR-1` / `task-907e8544-0693-4804-a14d-3f8a581728e0`
- run: `run-5c7e4c46-c858-4434-9196-4bd2d0625815`
- final state: `done true`, `canceled false`, run completed, no interruption

CLI fake-provider injection remains deferred; automated fake-provider workflow coverage currently lives in backend tests.
