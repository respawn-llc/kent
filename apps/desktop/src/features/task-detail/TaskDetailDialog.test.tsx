import { createBrowserNativeBridge, type NativeBridge, type NativeBuilderSessionLaunch } from "@builder/desktop-native-bridge";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

import { App } from "../../App";
import type { JsonObject, JsonValue } from "../../api/json";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";

describe("TaskDetailDialog", () => {
  it("answers questions, approves transitions, comments, and teleports through native bridge", async () => {
    window.history.pushState(null, "", "/tasks/task-1");
    const launched: NativeBuilderSessionLaunch[] = [];
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.task.get", result: taskDetailResponse },
      { method: "workflow.task.activity.list", result: activityResponse },
      { method: "ask.listPendingBySession", result: pendingAskResponse },
      { method: "workflow.task.question.answer", result: {} },
      { method: "workflow.task.approve", result: {} },
      { method: "workflow.task.comment.add", result: commentAddResponse },
      { method: "workflow.task.teleportTarget.get", result: teleportResponse },
    ], nativeBridge(launched));

    render(<App services={services} />);

    const recommendedOption = await screen.findByRole("button", { name: /Use option A/u });
    expect(recommendedOption).toBeInTheDocument();
    fireEvent.click(recommendedOption);
    fireEvent.click(screen.getByRole("button", { name: "Submit answer" }));

    await waitFor(() => {
      const params = callParams(services.transport.calls, "workflow.task.question.answer");
      expect(params.ask_id).toBe("ask-1");
      expect(params.client_request_id).toMatch(/^gui-question-ask-1-/u);
      expect(params.freeform_answer).toBe("");
      expect(params.run_id).toBe("run-1");
      expect(params.selected_option_number).toBe(1);
      expect(params.task_id).toBe("task-1");
    });

    expect(screen.queryByRole("button", { name: "Reject" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.approve",
        params: { task_transition_id: "transition-1" },
      });
    });

    fireEvent.change(screen.getByLabelText("Add comment"), { target: { value: "Fresh comment" } });
    fireEvent.click(screen.getByRole("button", { name: "Add comment" }));
    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.comment.add",
        params: { author: "GUI", body: "Fresh comment", task_id: "task-1" },
      });
    });

    fireEvent.click(screen.getByRole("button", { name: "Teleport" }));
    await waitFor(() => {
      expect(launched).toEqual([{ sessionId: "session-teleport", cwd: "/tmp/worktree/subdir" }]);
    });
  });
});

function nativeBridge(launched: NativeBuilderSessionLaunch[]): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    capabilities: {
      ...base.capabilities,
      terminal: { launchBuilderSession: true },
    },
    terminal: {
      async launchBuilderSession(target): Promise<void> {
        launched.push(target);
      },
    },
  };
}

const workflow = {
  workflow_id: "workflow-1",
  display_name: "Delivery",
  description: "",
  graph_revision: 1,
  is_project_default: true,
  valid_for_task_creation: true,
  validation_errors: [],
};

const workspace = {
  workspace_id: "workspace-1",
  display_name: "Main",
  root_path: "/tmp/project",
  availability: "available",
  is_primary: true,
  updated_at_unix_ms: 1,
};

const taskActions = {
  can_start: false,
  can_interrupt: true,
  interrupt_run_id: "run-1",
  can_resume: false,
  resume_run_id: "",
  can_cancel: true,
  needs_detail_for_interrupt: false,
  needs_detail_for_resume: false,
};

const attentionBase = {
  project_id: "project-1",
  workflow_id: "workflow-1",
  task_id: "task-1",
  task_short_id: "T-1",
  task_title: "Resolve blocker",
  occurred_at_unix_ms: 1,
  latest_event_sequence: 1,
};

const taskDetailResponse = {
  task: {
    summary: {
      id: "task-1",
      project_id: "project-1",
      workflow_id: "workflow-1",
      short_id: "T-1",
      title: "Resolve blocker",
      created_at_unix_ms: 1,
      updated_at_unix_ms: 2,
      done: false,
      canceled_at_unix_ms: 0,
    },
    project: { display_name: "Project" },
    workflow,
    body: "Need operator input",
    source_workspace: workspace,
    managed_worktree: { root_path: "/tmp/worktree" },
    status: { kind: "running", label: "Running", native_state: "running", node_ids: ["node-1"], run_ids: ["run-1"], attention_types: ["question", "approval"] },
    actions: taskActions,
    attention: [
      {
        ...attentionBase,
        id: "attention-question",
        kind: "question",
        run_id: "run-1",
        session_id: "session-1",
        ask_id: "ask-1",
        task_transition_id: "",
        message: "Pick answer",
      },
      {
        ...attentionBase,
        id: "attention-approval",
        kind: "approval",
        run_id: "run-1",
        session_id: "session-1",
        ask_id: "",
        task_transition_id: "transition-1",
        message: "Approve transition",
      },
    ],
    runs: [
      {
        id: "run-1",
        task_id: "task-1",
        placement_id: "placement-1",
        node_id: "node-1",
        session_id: "session-1",
        session_name: "Builder session",
        role: "agent",
        status: "running",
        generation: 1,
        waiting_ask_id: "ask-1",
        started_at_unix_ms: 1,
        completed_at_unix_ms: 0,
        interrupted_at_unix_ms: 0,
      },
    ],
    transitions: [
      {
        id: "transition-1",
        transition_id: "ship",
        transition_display_name: "Ship",
        source_node_display_name: "Implement",
        state: "pending_approval",
        commentary: "Looks good",
        output_values: { result: "ok" },
        edges: [],
        workflow_revision_seen: 7,
        created_at_unix_ms: 2,
        applied_at_unix_ms: 0,
      },
    ],
    comments: [
      {
        id: "comment-1",
        task_id: "task-1",
        body: "Existing comment",
        author: "GUI",
        deleted_at_unix_ms: 0,
        created_at_unix_ms: 1,
        updated_at_unix_ms: 1,
      },
    ],
  },
};

const activityResponse = {
  items: [
    {
      activity_id: "activity-1",
      type: "comment",
      task_id: "task-1",
      occurred_at_unix_ms: 2,
      updated_at_unix_ms: 2,
      actor: "GUI",
      summary: "Comment added",
      comment: null,
      transition: null,
      run: null,
      attention: null,
    },
  ],
  next_page_token: "",
  generated_at_unix_ms: 3,
};

const pendingAskResponse = {
  Asks: [
    {
      AskID: "ask-1",
      SessionID: "session-1",
      Question: "Choose path",
      Suggestions: ["Use option A", "Use option B"],
      RecommendedOptionIndex: 1,
      CreatedAt: "2026-05-16T00:00:00Z",
    },
  ],
};

const commentAddResponse = {
  comment: {
    id: "comment-2",
    task_id: "task-1",
    body: "Fresh comment",
    author: "GUI",
    deleted_at_unix_ms: 0,
    created_at_unix_ms: 4,
    updated_at_unix_ms: 4,
  },
};

const teleportResponse = {
  available: true,
  task_id: "task-1",
  run_id: "run-1",
  session_id: "session-teleport",
  project_id: "project-1",
  workspace_id: "workspace-1",
  worktree_id: "worktree-1",
  cwd_relpath: "subdir",
  failure_reason: "",
};

function callParams(calls: readonly Readonly<{ method: string; params: JsonValue }>[], method: string): JsonObject {
  const params = calls.find((call) => call.method === method)?.params;
  if (!isJsonObject(params)) {
    throw new Error(`Missing object params for ${method}.`);
  }
  return params;
}

function isJsonObject(value: JsonValue | undefined): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
