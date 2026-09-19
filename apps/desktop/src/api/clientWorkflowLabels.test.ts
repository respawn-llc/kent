import * as wf from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import * as taskRead from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import * as taskLifecycle from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { create } from "@app/server-api-contract";
import { unexpectedProjectOverflow } from "@/test-support/api";
import { FakeRpcTransport } from "@/test-support/api";
import { ApiClient } from "./client";
import { ContractError } from "./errors";
import { taskLabelFilterPayload } from "./clientWorkflowLabels";
const priorityID = "f74ce532-9e6e-4cf6-b3c1-d67d5a3eedcf";
const urgentID = "942495c2-5958-4959-8445-94046ad74fbd";
const smallID = "11111111-1111-4111-8111-111111111111";
describe("ApiClient workflow labels", () => {
  it("loads each Project Task-group definition exactly once", async () => {
    const definitions = [
      { group: "active", status_kinds: ["running", "active"] },
      { group: "backlog", status_kinds: ["backlog"] },
      { group: "done", status_kinds: ["done"] },
    ] as const;
    const result = {
      project_id: "project-1",
      definitions,
      counts: { active: 3, backlog: 2, done: 1 },
      generated_at_unix_ms: 7,
    };
    const getCounts = async (response: unknown) =>
      new ApiClient(
        new FakeRpcTransport([{ method: "workflow.task.groupCounts", result: response }]),
        unexpectedProjectOverflow,
      ).getProjectTaskGroupCounts({ projectID: "project-1" });
    await expect(getCounts(result)).resolves.toMatchObject({
      definitions: definitions.map(({ group, status_kinds }) => ({ group, statusKinds: status_kinds })),
      counts: result.counts,
    });
    await expect(
      getCounts({ ...result, definitions: [definitions[0], definitions[0], definitions[2]] }),
    ).rejects.toBeInstanceOf(ContractError);
  });
  it("reorders a Project label catalog and preserves the authoritative response order", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: wf.ProjectLabelService.method.reorder,
        result: create(wf.ProjectLabelService.method.reorder.output, {
          outcome: {
            case: "success",
            value: {
              catalog: {
                projectId: "project-1",
                labels: [
                  {
                    id: urgentID,
                    name: "Urgent",
                  },
                  {
                    id: priorityID,
                    name: "Priority",
                  },
                ],
              },
            },
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(client.reorderProjectLabels("project-1", [urgentID, priorityID])).resolves.toEqual({
      projectID: "project-1",
      labels: [
        { id: urgentID, name: "Urgent" },
        { id: priorityID, name: "Priority" },
      ],
    });
    expect(transport.descriptorCalls).toEqual([
      {
        descriptor: wf.ProjectLabelService.method.reorder,
        request: create(wf.ProjectLabelService.method.reorder.input, {
          projectId: "project-1",
          labelIds: [urgentID, priorityID],
        }),
      },
    ]);
  });
  it("creates a related task through an atomic relationship-intent collection and returns its summary", async () => {
    const transport = new FakeRpcTransport([
      {
        method: "workflow.task.create",
        result: {
          task: {
            id: "task-new",
            short_id: "KENT-42",
            title: "New blocker",
            workflow_id: smallID,
          },
        },
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(
      client.createTask({
        projectID: "project-1",
        workflowID: smallID,
        title: "New blocker",
        body: "",
        sourceWorkspaceID: "workspace-origin",
        labelIDs: [],
        dependencyIntents: [
          { relatedTaskID: "task-blocked", newTaskRole: "blocker" },
          { relatedTaskID: "task-blocker", newTaskRole: "blocked" },
        ],
      }),
    ).resolves.toEqual({
      id: "task-new",
      shortID: "KENT-42",
      title: "New blocker",
      workflowID: smallID,
    });
    expect(transport.calls).toEqual([
      {
        method: "workflow.task.create",
        params: {
          project_id: "project-1",
          workflow_id: smallID,
          title: "New blocker",
          body: "",
          source_workspace_id: "workspace-origin",
          label_ids: [],
          dependency_intents: [
            { related_task_id: "task-blocked", new_task_role: "blocker" },
            { related_task_id: "task-blocker", new_task_role: "blocked" },
          ],
        },
      },
    ]);
  });
  it("omits an empty excluded partition from a named filter payload", () => {
    expect(
      taskLabelFilterPayload({
        kind: "named",
        mode: "any",
        labelIDs: [priorityID],
        excludedLabelIDs: [],
      }),
    ).toEqual({
      kind: "named",
      named: {
        mode: "any",
        label_ids: [priorityID],
      },
    });
  });
  it("lists the complete bounded Project label catalog", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: wf.ProjectLabelService.method.list,
        result: create(wf.ProjectLabelService.method.list.output, {
          outcome: {
            case: "success",
            value: {
              catalog: {
                projectId: "project-1",
                labels: [
                  {
                    id: priorityID,
                    name: "Priority",
                  },
                ],
              },
            },
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(client.listProjectLabels("project-1")).resolves.toEqual({
      projectID: "project-1",
      labels: [{ id: priorityID, name: "Priority" }],
    });
    expect(transport.descriptorCalls).toEqual([
      {
        descriptor: wf.ProjectLabelService.method.list,
        request: create(wf.ProjectLabelService.method.list.input, {
          projectId: "project-1",
        }),
      },
    ]);
  });
  it("creates a Project label and returns the authoritative label", async () => {
    const rawName = ` ${"e\u0301".repeat(64)} `;
    const normalizedName = "é".repeat(64);
    const transport = new FakeRpcTransport([
      {
        descriptor: wf.ProjectLabelService.method.create,
        result: create(wf.ProjectLabelService.method.create.output, {
          outcome: {
            case: "success",
            value: {
              label: {
                id: priorityID,
                name: normalizedName,
              },
            },
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(client.createProjectLabel("project-1", rawName)).resolves.toEqual({
      id: priorityID,
      name: normalizedName,
    });
    expect(transport.descriptorCalls).toEqual([
      {
        descriptor: wf.ProjectLabelService.method.create,
        request: create(wf.ProjectLabelService.method.create.input, {
          projectId: "project-1",
          name: rawName,
        }),
      },
    ]);
  });
  it("renames a Project label without changing its identity", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: wf.ProjectLabelService.method.rename,
        result: create(wf.ProjectLabelService.method.rename.output, {
          outcome: {
            case: "success",
            value: {
              label: {
                id: priorityID,
                name: "Urgent",
              },
            },
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(client.renameProjectLabel("project-1", priorityID, "Urgent")).resolves.toEqual({
      id: priorityID,
      name: "Urgent",
    });
    expect(transport.descriptorCalls).toEqual([
      {
        descriptor: wf.ProjectLabelService.method.rename,
        request: create(wf.ProjectLabelService.method.rename.input, {
          projectId: "project-1",
          labelId: priorityID,
          name: "Urgent",
        }),
      },
    ]);
  });
  it("deletes a Project label and returns its authoritative identity", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: wf.ProjectLabelService.method.delete,
        result: create(wf.ProjectLabelService.method.delete.output, {
          outcome: {
            case: "success",
            value: {
              labelId: priorityID,
            },
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(client.deleteProjectLabel("project-1", priorityID)).resolves.toBe(priorityID);
    expect(transport.descriptorCalls).toEqual([
      {
        descriptor: wf.ProjectLabelService.method.delete,
        request: create(wf.ProjectLabelService.method.delete.input, {
          projectId: "project-1",
          labelId: priorityID,
        }),
      },
    ]);
  });
  it("reads and updates the authoritative task label assignment", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: taskRead.TaskLabelReadService.method.get,
        result: create(taskRead.TaskLabelReadService.method.get.output, {
          outcome: {
            case: "success",
            value: {
              assignment: {
                taskId: "task-1",
                labelIds: [priorityID],
              },
            },
          },
        }),
      },
      {
        descriptor: taskLifecycle.TaskLabelService.method.update,
        result: create(taskLifecycle.TaskLabelService.method.update.output, {
          outcome: {
            case: "success",
            value: {
              assignment: {
                taskId: "task-1",
                labelIds: [urgentID],
              },
            },
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(client.getTaskLabels("task-1")).resolves.toEqual({
      taskID: "task-1",
      labelIDs: [priorityID],
    });
    await expect(client.updateTaskLabels("task-1", [urgentID], [priorityID])).resolves.toEqual({
      taskID: "task-1",
      labelIDs: [urgentID],
    });
    expect(transport.descriptorCalls).toEqual([
      {
        descriptor: taskRead.TaskLabelReadService.method.get,
        request: create(taskRead.TaskLabelReadService.method.get.input, {
          taskId: "task-1",
        }),
      },
      {
        descriptor: taskLifecycle.TaskLabelService.method.update,
        request: create(taskLifecycle.TaskLabelService.method.update.input, {
          taskId: "task-1",
          addLabelIds: [urgentID],
          removeLabelIds: [priorityID],
        }),
      },
    ]);
  });
  it("creates a task with an explicit atomic label assignment", async () => {
    const transport = new FakeRpcTransport([
      {
        method: "workflow.task.create",
        result: {
          task: {
            id: "task-1",
            short_id: "KENT-1",
            title: "Ship labels",
            workflow_id: "11111111-1111-4111-8111-111111111111",
          },
        },
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(
      client.createTask({
        projectID: "project-1",
        workflowID: "11111111-1111-4111-8111-111111111111",
        title: "Ship labels",
        body: "Wire the desktop API.",
        sourceWorkspaceID: "workspace-1",
        labelIDs: [priorityID],
        dependencyIntents: [],
      }),
    ).resolves.toEqual({
      id: "task-1",
      shortID: "KENT-1",
      title: "Ship labels",
      workflowID: "11111111-1111-4111-8111-111111111111",
    });
    expect(transport.calls).toEqual([
      {
        method: "workflow.task.create",
        params: {
          project_id: "project-1",
          workflow_id: "11111111-1111-4111-8111-111111111111",
          title: "Ship labels",
          body: "Wire the desktop API.",
          source_workspace_id: "workspace-1",
          label_ids: [priorityID],
          dependency_intents: [],
        },
      },
    ]);
  });
  it("rejects malformed and prefixed Workflow IDs before task RPCs", async () => {
    const transport = new FakeRpcTransport([]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(
      client.createTask({
        projectID: "project-1",
        workflowID: "not-a-workflow-id",
        title: "Ship labels",
        body: "",
        sourceWorkspaceID: "workspace-1",
        labelIDs: [],
        dependencyIntents: [],
      }),
    ).rejects.toThrow();
    await expect(
      client.listTasks({
        projectID: "project-1",
        workflowID: "workflow-11111111-1111-4111-8111-111111111111",
        labelFilter: { kind: "none" },
        limit: 25,
      }),
    ).rejects.toThrow();
    expect(transport.calls).toEqual([]);
  });
  it("lists label-filtered task projections with ordered Label display data", async () => {
    const transport = new FakeRpcTransport([
      {
        method: "workflow.task.list",
        result: {
          scope: { project_id: "project-1", workflow_id: "11111111-1111-4111-8111-111111111111" },
          matching_workflow_cardinality: "one",
          next_offset: null,
          generated_at_unix_ms: 7,
          tasks: [
            {
              task_id: "task-1",
              short_id: "PROJ-1",
              workflow_id: "11111111-1111-4111-8111-111111111111",
              workflow_name: "Delivery",
              title: "Ship labels",
              created_at_unix_ms: 1,
              updated_at_unix_ms: 2,
              column_keys: ["implement"],
              status: {
                kind: "active",
                native_state: "active",
                node_ids: ["node-1"],
                attention_types: [],
              },
              labels: [{ id: priorityID, name: "Priority" }],
              dependency_progress: { satisfied_count: 1, total_count: 2 },
            },
          ],
        },
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(
      client.listTasks({
        projectID: "project-1",
        workflowID: "11111111-1111-4111-8111-111111111111",
        group: "active",
        labelFilter: {
          kind: "named",
          mode: "any",
          labelIDs: [priorityID, urgentID],
          excludedLabelIDs: [smallID],
        },
        limit: 25,
      }),
    ).resolves.toMatchObject({
      scope: { projectID: "project-1", workflowID: "11111111-1111-4111-8111-111111111111" },
      matchingWorkflowCardinality: "one",
      tasks: [
        {
          id: "task-1",
          labels: [{ id: priorityID, name: "Priority" }],
          dependencyProgress: { satisfiedCount: 1, totalCount: 2 },
        },
      ],
    });
    expect(transport.calls).toEqual([
      {
        method: "workflow.task.list",
        params: {
          project_id: "project-1",
          workflow_id: "11111111-1111-4111-8111-111111111111",
          group: "active",
          column_keys: [],
          status_kinds: [],
          attention_kinds: [],
          label_filter: {
            kind: "named",
            named: {
              mode: "any",
              label_ids: [urgentID, priorityID],
              excluded_label_ids: [smallID],
            },
          },
          sort: [],
          offset: 0,
          limit: 25,
        },
      },
    ]);
  });
  it("rejects a zero task-list continuation offset", async () => {
    const client = new ApiClient(
      new FakeRpcTransport([
        {
          method: "workflow.task.list",
          result: {
            scope: { project_id: "project-1" },
            matching_workflow_cardinality: "none",
            next_offset: 0,
            generated_at_unix_ms: 7,
            tasks: [],
          },
        },
      ]),
      unexpectedProjectOverflow,
    );
    await expect(
      client.listTasks({
        projectID: "project-1",
        labelFilter: { kind: "none" },
      }),
    ).rejects.toBeInstanceOf(ContractError);
  });
});
