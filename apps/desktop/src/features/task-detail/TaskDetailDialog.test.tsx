/* eslint-disable max-lines -- Task detail route tests keep representative detail/native bridge fixtures local. */
/* eslint-disable testing-library/no-node-access -- Dialog tests verify native window dimensions and focus ownership. */
import {
  createBrowserNativeBridge,
  type NativeBridge,
  type NativeBuilderSessionLaunch,
  type NativeTaskDetailChanged,
  type NativeTaskDetailTarget,
} from "@builder/desktop-native-bridge";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";

import { App } from "../../App";
import type { JsonObject, JsonValue } from "../../api/json";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";

describe("TaskDetailDialog", () => {
  it("renders direct task route inline with inbox, comments, approvals, questions, and CLI actions", async () => {
    window.history.pushState(null, "", "/tasks/task-1");
    const launched: NativeBuilderSessionLaunch[] = [];
    const services = createTestServices(
      [
        ...startupRoutes,
        { method: "workflow.task.get", result: taskDetailResponse },
        { method: "workflow.task.activity.list", result: activityResponse },
        { method: "ask.listPendingBySession", result: pendingAskResponse },
        { method: "workflow.task.question.answer", result: {} },
        { method: "workflow.task.approve", result: {} },
        { method: "workflow.task.comment.add", result: commentAddResponse },
        { method: "workflow.task.comment.replace", result: {} },
        { method: "workflow.task.teleportTarget.get", result: teleportResponse },
      ],
      nativeBridge(launched),
    );

    render(<App services={services} />);

    const recommendedOption = await screen.findByRole("radio", { name: /Use option A/u });
    expect(screen.getByText("Choose path")).toBeInTheDocument();
    expect(screen.queryByText("Pick answer")).not.toBeInTheDocument();
    expect(screen.getByTestId("route-transition-frame")).toHaveClass("p-[var(--space-2)]");
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

    expect(screen.getAllByLabelText("Add comment")).toHaveLength(1);
    fireEvent.change(screen.getByLabelText("Add comment"), {
      target: { value: "Fresh comment" },
    });
    expect(screen.getByTestId("task-comment-save")).not.toHaveClass("absolute");
    expect(screen.getByTestId("task-comment-save").parentElement).toBe(
      screen.getByTestId("task-comment-input-frame"),
    );
    expect(screen.getByTestId("task-comment-save")).toHaveStyle({
      marginBottom: "var(--space-2)",
      marginRight: "var(--space-2)",
    });
    fireEvent.click(screen.getByRole("button", { name: "Submit comment" }));
    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.comment.add",
        params: { author: "GUI", body: "Fresh comment", task_id: "task-1" },
      });
    });

    fireEvent.click(screen.getByRole("button", { name: "Edit comment" }));
    expect(screen.getAllByLabelText("Edit comment")).toHaveLength(1);
    fireEvent.change(screen.getByLabelText("Edit comment"), {
      target: { value: "Edited comment" },
    });
    expect(screen.getByTestId("task-comment-save")).not.toHaveClass("absolute");
    expect(screen.getByTestId("task-comment-save").parentElement).toBe(
      screen.getByTestId("task-comment-input-frame"),
    );
    expect(screen.getByTestId("task-comment-save")).toHaveStyle({
      marginBottom: "var(--space-2)",
      marginRight: "var(--space-2)",
    });
    fireEvent.click(screen.getByRole("button", { name: "Save comment" }));
    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.comment.replace",
        params: { body: "Edited comment", comment_id: "comment-1" },
      });
    });

    fireEvent.click(screen.getByRole("button", { name: "Open in CLI" }));
    await waitFor(() => {
      expect(launched).toEqual([{ sessionId: "session-teleport", cwd: "/tmp/worktree/subdir" }]);
    });
  });

  it("renders task detail as islands and edits active task title and description", async () => {
    window.history.pushState(null, "", "/tasks/task-1");
    let currentTitle = taskDetailNoInboxResponse.task.summary.title;
    let currentBody = taskDetailNoInboxResponse.task.body;
    const services = createTestServices([
      ...startupRoutes,
      {
        method: "workflow.task.get",
        handler: () => ({
          task: {
            ...taskDetailNoInboxResponse.task,
            summary: { ...taskDetailNoInboxResponse.task.summary, title: currentTitle },
            body: currentBody,
          },
        }),
      },
      { method: "workflow.task.activity.list", result: activityResponse },
      {
        method: "workflow.task.update",
        handler: (params: JsonValue) => {
          if (isJsonObject(params)) {
            currentTitle = typeof params.title === "string" ? params.title : currentTitle;
            currentBody = typeof params.body === "string" ? params.body : currentBody;
          }
          return taskUpdateResponse;
        },
      },
    ]);

    render(<App services={services} />);

    expect(await screen.findByRole("textbox", { name: "Title" })).toHaveValue("Resolve blocker");
    expect(screen.getByText("T-1")).toHaveClass("uppercase");
    expect(screen.queryByRole("region", { name: "Inbox" })).not.toBeInTheDocument();
    expect(screen.getByText("Project:", { exact: false })).toBeInTheDocument();
    expect(screen.getByText("Status:", { exact: false })).toBeInTheDocument();
    expect(screen.getByText("Workspace:", { exact: false })).toBeInTheDocument();
    expect(screen.getByText("Workflow:", { exact: false })).toBeInTheDocument();
    expect(screen.getByText("Sessions:", { exact: false })).toBeInTheDocument();
    expect(screen.getByTestId("task-detail-island-stack")).toHaveClass("gap-[var(--space-2)]");
    expect(screen.getByTestId("task-detail-body-split")).toHaveClass("gap-[var(--space-2)]");
    expect(screen.getByTestId("task-detail-body-split")).not.toHaveClass("items-start");
    expect(screen.getByTestId("task-detail-body-split")).toHaveClass(
      "min-[512px]:grid-cols-[minmax(0,7fr)_minmax(260px,3fr)]",
    );
    expect(screen.getByTestId("task-detail-title-island")).toHaveClass(
      "px-[var(--space-4)]",
      "py-[var(--space-2)]",
    );
    expect(screen.getByTestId("task-detail-title-island")).not.toHaveClass("p-[var(--space-2)]");
    const titleInput = within(screen.getByTestId("task-detail-title-island")).getByRole("textbox", {
      name: "Title",
    });
    const titleForm = titleInput.closest("form");
    if (titleForm === null) {
      throw new Error("expected title textbox to be inside the title island form");
    }
    expect(titleForm.firstElementChild).toBe(titleInput);
    expect(titleForm.lastElementChild).toHaveTextContent("T-1");
    expect(screen.getByTestId("task-description-save")).toHaveClass("opacity-0", "pointer-events-none");
    expect(screen.getByTestId("task-description-save")).not.toHaveClass("absolute");
    expect(screen.getByTestId("task-description-save").parentElement).toBe(
      screen.getByTestId("task-description-input-frame"),
    );
    expect(screen.getByTestId("task-description-save")).toHaveStyle({
      marginBottom: "var(--space-2)",
      marginRight: "var(--space-2)",
    });
    expect(screen.getByTestId("task-description-save")).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Save changes" })).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Renamed task" } });
    expect(screen.getByRole("button", { name: "Save title" })).toBeInTheDocument();
    expect(screen.getByTestId("task-description-save")).toHaveClass("opacity-0", "pointer-events-none");
    expect(screen.queryByRole("button", { name: "Save changes" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Save title" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.update",
        params: {
          task_id: "task-1",
          title: "Renamed task",
          body: "Need operator input",
        },
      });
    });

    fireEvent.change(screen.getByRole("textbox", { name: "Description" }), {
      target: { value: "Updated details" },
    });
    expect(screen.getByTestId("task-description-save")).toHaveClass("opacity-100");
    expect(screen.getByTestId("task-description-save")).toHaveStyle({
      marginBottom: "var(--space-2)",
      marginRight: "var(--space-2)",
    });
    expect(screen.getByTestId("task-description-save")).not.toHaveClass("pointer-events-none");
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.update",
        params: {
          task_id: "task-1",
          title: "Renamed task",
          body: "Updated details",
        },
      });
    });
  });

  it("keeps the description save control anchored to the textarea when properties are taller", async () => {
    window.history.pushState(null, "", "/tasks/task-1");
    const longRunID = "4999ea41-e5e2-43be-8735-9ce37eefd661";
    const services = createTestServices([
      ...startupRoutes,
      {
        method: "workflow.task.get",
        result: {
          task: {
            ...taskDetailNoInboxResponse.task,
            runs: [
              {
                ...taskDetailNoInboxResponse.task.runs[0],
                id: longRunID,
                completed_at_unix_ms: 0,
                interrupted_at_unix_ms: 0,
              },
            ],
          },
        },
      },
      { method: "workflow.task.activity.list", result: activityResponse },
    ]);

    render(<App services={services} />);

    expect(
      await screen.findByRole("button", { name: new RegExp(`Interrupt ${longRunID}`, "u") }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("task-detail-body-split")).not.toHaveClass("items-start");
    expect(screen.getByTestId("task-description-save")).not.toHaveClass("absolute");
    expect(screen.getByTestId("task-description-save").parentElement).toBe(
      screen.getByTestId("task-description-input-frame"),
    );
    expect(screen.getByTestId("task-description-save")).toHaveStyle({
      marginBottom: "var(--space-2)",
      marginRight: "var(--space-2)",
    });
  });

  it("teleports source-workspace tasks from the source workspace root", async () => {
    window.history.pushState(null, "", "/tasks/task-1");
    const launched: NativeBuilderSessionLaunch[] = [];
    const services = createTestServices(
      [
        ...startupRoutes,
        {
          method: "workflow.task.get",
          result: {
            task: {
              ...taskDetailResponse.task,
              managed_worktree: null,
            },
          },
        },
        { method: "workflow.task.activity.list", result: activityResponse },
        {
          method: "workflow.task.teleportTarget.get",
          result: {
            ...teleportResponse,
            worktree_id: "",
            cwd_relpath: "pkg/gui",
          },
        },
      ],
      nativeBridge(launched),
    );

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "Open in CLI" }));
    await waitFor(() => {
      expect(launched).toEqual([{ sessionId: "session-teleport", cwd: "/tmp/project/pkg/gui" }]);
    });
  });

  it("opens Home Inbox rows through native task detail window when available", async () => {
    window.history.pushState(null, "", "/");
    const opened: NativeTaskDetailTarget[] = [];
    const services = createTestServices(
      [
        ...startupRoutes,
        {
          method: "workflow.attention.list",
          result: {
            items: [
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
            ],
            next_page_token: "",
            generated_at_unix_ms: 1,
          },
        },
      ],
      nativeBridgeWithTaskDetailWindow(opened),
    );

    render(<App services={services} />);

    fireEvent.click(await screen.findByTestId("attention-row"));
    await waitFor(() => {
      expect(opened).toEqual([{ resumeRunId: "", taskId: "task-1" }]);
    });
  });

  it("refreshes visible Home Inbox queries after native task detail mutations", async () => {
    window.history.pushState(null, "", "/");
    let onChanged: ((event: NativeTaskDetailChanged) => void) | null = null;
    const services = createTestServices(
      [
        ...startupRoutes,
        {
          method: "workflow.attention.list",
          handler: (_params, callIndex) => ({
            items: callIndex === 0 ? [attentionResponseItem] : [],
            next_page_token: "",
            generated_at_unix_ms: callIndex + 1,
          }),
        },
      ],
      nativeBridgeWithTaskDetailChangeHandler((handler) => {
        onChanged = handler;
      }),
    );

    render(<App services={services} />);
    expect(await screen.findByTestId("attention-row")).toBeInTheDocument();

    act(() => {
      onChanged?.({ taskId: "task-1" });
    });

    await waitFor(() => {
      expect(screen.queryByTestId("attention-row")).not.toBeInTheDocument();
    });
  });

  it("uses the large task detail content width in native dialog windows", async () => {
    window.history.pushState(null, "", "/native-dialog/task-detail?taskId=task-1&resumeRunId=");
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.task.get", result: taskDetailResponse },
      { method: "workflow.task.activity.list", result: activityResponse },
      { method: "ask.listPendingBySession", result: pendingAskResponse },
    ]);

    render(<App services={services} />);

    expect(await screen.findByRole("dialog", { name: "Task" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Task" })).not.toBeInTheDocument();
    expect(screen.getByTestId("native-dialog-content")).toHaveStyle({ maxWidth: "1200px" });
    expect(screen.getByTestId("native-dialog-scrollport")).toHaveClass(
      "p-[var(--space-2)]",
    );
    expect(screen.getByTestId("native-dialog-scrollport").closest("main")).toHaveClass(
      "pt-[var(--native-titlebar-height)]",
    );
    expect(screen.getByTestId("native-dialog-scrollport").closest("main")).not.toHaveClass(
      "p-[var(--native-titlebar-height)_var(--space-2)_var(--space-2)]",
    );
  });

  it("shows plain backend and native teleport failures", async () => {
    window.history.pushState(null, "", "/tasks/task-1");
    const services = createTestServices(
      [
        ...startupRoutes,
        { method: "workflow.task.get", result: taskDetailNoInboxResponse },
        { method: "workflow.task.activity.list", result: activityResponse },
        {
          method: "workflow.task.teleportTarget.get",
          result: { ...teleportResponse, available: false, failure_reason: "No active session." },
        },
      ],
      nativeBridge([]),
    );

    const view = render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "Open in CLI" }));
    expect(await screen.findByText("No active session.")).toBeInTheDocument();
    view.unmount();

    window.history.pushState(null, "", "/tasks/task-1");
    const failingServices = createTestServices(
      [
        ...startupRoutes,
        { method: "workflow.task.get", result: taskDetailNoInboxResponse },
        { method: "workflow.task.activity.list", result: activityResponse },
        { method: "workflow.task.teleportTarget.get", result: teleportResponse },
      ],
      failingTerminalBridge("Local Builder executable is unavailable."),
    );

    render(<App services={failingServices} />);

    fireEvent.click(await screen.findByRole("button", { name: "Open in CLI" }));
    expect(await screen.findByText("Local Builder executable is unavailable.")).toBeInTheDocument();
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

function nativeBridgeWithTaskDetailWindow(opened: NativeTaskDetailTarget[]): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    capabilities: {
      ...base.capabilities,
      taskDetailWindow: true,
    },
    taskDetail: {
      ...base.taskDetail,
      async openWindow(target): Promise<void> {
        opened.push(target);
      },
    },
  };
}

function failingTerminalBridge(message: string): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    capabilities: {
      ...base.capabilities,
      terminal: { launchBuilderSession: true },
    },
    terminal: {
      async launchBuilderSession(): Promise<void> {
        throw new Error(message);
      },
    },
  };
}

function nativeBridgeWithTaskDetailChangeHandler(
  onRegistered: (handler: (event: NativeTaskDetailChanged) => void) => void,
): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    taskDetail: {
      ...base.taskDetail,
      async onChanged(handler): Promise<() => void> {
        onRegistered(handler);
        return () => undefined;
      },
    },
  };
}

const workflow = {
  workflow_id: "workflow-1",
  display_name: "Delivery",
  description: "",
  version: 1,
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
};

const attentionResponseItem = {
  ...attentionBase,
  id: "attention-question",
  kind: "question",
  run_id: "run-1",
  session_id: "session-1",
  ask_id: "ask-1",
  task_transition_id: "",
  message: "Pick answer",
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
    managed_worktree: { canonical_root: "/tmp/worktree" },
    status: {
      kind: "running",
      label: "Running",
      native_state: "running",
      node_ids: ["node-1"],
      run_ids: ["run-1"],
      attention_types: ["question", "approval"],
    },
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
        message: "",
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
        created_at_unix_ms: 1,
        updated_at_unix_ms: 1,
      },
    ],
  },
};

const taskDetailNoInboxResponse = {
  task: {
    ...taskDetailResponse.task,
    attention: [],
    transitions: [],
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
    created_at_unix_ms: 4,
    updated_at_unix_ms: 4,
  },
};

const taskUpdateResponse = {
  task: {
    id: "task-1",
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

function callParams(
  calls: readonly Readonly<{ method: string; params: JsonValue }>[],
  method: string,
): JsonObject {
  const params = calls.find((call) => call.method === method)?.params;
  if (!isJsonObject(params)) {
    throw new Error(`Missing object params for ${method}.`);
  }
  return params;
}

function isJsonObject(value: JsonValue | undefined): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
