import { BuilderApiClient } from "./client";
import { ContractError } from "./errors";
import { FakeRpcTransport } from "./fakeTransport";
import { protocolVersion } from "./jsonRpcSocket";

describe("BuilderApiClient", () => {
  it("parses readiness and sends mutation params through typed method boundary", async () => {
    const transport = new FakeRpcTransport([
      {
        method: "server.readiness.get",
        result: {
          ready: true,
          server_id: "server-1",
          server_version: "1.3.0",
          protocol_version: protocolVersion,
          auth_ready: true,
          auth_required: false,
          endpoint: "ws://127.0.0.1:53082/rpc",
        },
      },
      { method: "workflow.task.start", result: {} },
    ]);
    const client = new BuilderApiClient(transport);

    await expect(client.getReadiness()).resolves.toMatchObject({
      ready: true,
      serverID: "server-1",
      serverVersion: "1.3.0",
      protocolVersion: protocolVersion,
    });
    await client.startTask("task-1");

    expect(transport.calls).toContainEqual({ method: "workflow.task.start", params: { task_id: "task-1" } });
  });

  it("rejects server contract drift before feature code receives raw data", async () => {
    const client = new BuilderApiClient(
      new FakeRpcTransport([{ method: "server.readiness.get", result: { ready: true } }]),
    );

    await expect(client.getReadiness()).rejects.toBeInstanceOf(ContractError);
  });

  it("normalizes empty workflow board metadata and node-card slices returned as null by Go JSON", async () => {
    const client = new BuilderApiClient(
      new FakeRpcTransport([
        { method: "workflow.board.get", result: emptyBoardResponse },
        { method: "workflow.board.nodeCards.list", result: emptyBoardNodeCardsResponse },
      ]),
    );

    await expect(client.getBoard("project-1", "")).resolves.toMatchObject({
      projectID: "project-1",
      workflows: [],
      groups: [],
      columns: [],
    });
    await expect(client.listBoardNodeCards("project-1", "workflow-1", "node-1", "cursor-1")).resolves.toMatchObject({
      projectID: "project-1",
      workflowID: "workflow-1",
      nodeID: "node-1",
      cards: [],
      nextPageToken: "cursor-2",
    });
  });

  it("normalizes empty task detail slices returned as null by Go JSON", async () => {
    const client = new BuilderApiClient(
      new FakeRpcTransport([{ method: "workflow.task.get", result: emptyTaskDetailResponse }]),
    );

    await expect(client.getTask("task-1")).resolves.toMatchObject({
      id: "task-1",
      runs: [],
      transitions: [],
      comments: [],
      attention: [],
    });
  });

  it("uses project edit workspace pagination and mutation RPC contracts", async () => {
    const transport = new FakeRpcTransport([
      {
        method: "project.edit.get",
        result: {
          project_id: "project-1",
          project_key: "PROJ",
          display_name: "Project",
          default_workspace_id: "workspace-1",
          workspaces: [workspaceResponse],
          next_page_token: "cursor-2",
        },
      },
      { method: "project.update", result: { project: projectSummaryResponse } },
      { method: "project.defaultWorkspace.set", result: { project: projectSummaryResponse } },
      {
        method: "project.unlinkWorkspace",
        result: {
          project_id: "project-1",
          workspace_id: "workspace-1",
          unlinked: false,
          blockers: [{ code: "default_workspace", message: "Default workspace cannot be unlinked." }],
        },
      },
    ]);
    const client = new BuilderApiClient(transport);

    await expect(client.getProjectEdit("project-1", "cursor-1")).resolves.toMatchObject({
      projectID: "project-1",
      nextPageToken: "cursor-2",
    });
    await client.updateProject("project-1", "Renamed");
    await client.setDefaultWorkspace("project-1", "workspace-1");
    await expect(client.unlinkWorkspace("project-1", "workspace-1")).resolves.toMatchObject({
      unlinked: false,
      blockers: [{ code: "default_workspace", count: 0 }],
    });

    expect(transport.calls).toContainEqual({
      method: "project.edit.get",
      params: { project_id: "project-1", page_size: 100, page_token: "cursor-1" },
    });
    expect(transport.calls).toContainEqual({
      method: "project.update",
      params: { project_id: "project-1", display_name: "Renamed" },
    });
    expect(transport.calls).toContainEqual({
      method: "project.defaultWorkspace.set",
      params: { project_id: "project-1", workspace_id: "workspace-1" },
    });
    expect(transport.calls).toContainEqual({
      method: "project.unlinkWorkspace",
      params: { project_id: "project-1", workspace_id: "workspace-1" },
    });
  });
});

const emptyWorkflow = {
  workflow_id: "",
  display_name: "",
  description: "",
  graph_revision: 0,
  is_project_default: false,
  valid_for_task_creation: false,
  validation_errors: null,
};

const emptyBoardResponse = {
  board: {
    project_id: "project-1",
    project: { project_key: "proj", display_name: "Project" },
    selected_workflow: emptyWorkflow,
    workflows: null,
    groups: null,
    columns: null,
    generated_at_unix_ms: 1,
    latest_event_sequence: 1,
  },
};

const emptyBoardNodeCardsResponse = {
  project_id: "project-1",
  workflow_id: "workflow-1",
  node_id: "node-1",
  cards: null,
  next_page_token: "cursor-2",
  generated_at_unix_ms: 1,
  latest_event_sequence: 1,
};

const workspaceResponse = {
  workspace_id: "workspace-1",
  display_name: "Project",
  root_path: "/tmp/project",
  availability: "available",
  is_primary: true,
  updated_at_unix_ms: 1,
};

const projectSummaryResponse = {
  project_id: "project-1",
  project_key: "PROJ",
  display_name: "Project",
  primary_workspace: workspaceResponse,
  default_workflow_id: "workflow-1",
  default_workflow_name: "Delivery",
  default_workflow_valid: true,
  updated_at_unix_ms: 1,
  task_count: 0,
  attention_count: 0,
  workflow_count: 1,
};

const emptyTaskDetailResponse = {
  task: {
    summary: {
      id: "task-1",
      project_id: "project-1",
      workflow_id: "workflow-1",
      short_id: "PROJ-1",
      title: "Task",
      created_at_unix_ms: 1,
      updated_at_unix_ms: 1,
      done: false,
      canceled_at_unix_ms: 0,
    },
    project: {
      display_name: "Project",
    },
    workflow: {
      workflow_id: "workflow-1",
      display_name: "Delivery",
      description: "",
      graph_revision: 1,
      is_project_default: true,
      valid_for_task_creation: true,
      validation_errors: null,
    },
    body: "Body",
    source_workspace: workspaceResponse,
    status: {
      kind: "backlog",
      label: "Backlog",
      native_state: "active",
      node_ids: [],
      run_ids: [],
      attention_types: [],
    },
    actions: {
      can_start: true,
      can_interrupt: false,
      can_resume: false,
      can_cancel: true,
      needs_detail_for_interrupt: false,
      needs_detail_for_resume: false,
      manual_move_target_node_ids: [],
    },
    attention: null,
    runs: null,
    transitions: null,
    comments: null,
  },
};
