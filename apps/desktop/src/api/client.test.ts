import * as wf from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import { unexpectedProjectOverflow } from "@/test-support/api";
import { z } from "zod";
import { create } from "@app/server-api-contract";
import { ReadinessSeverity, ServerService } from "@app/server-api-contract/gen/kent/api/server/server_pb";
import { ApiClient } from "./client";
import { FakeRpcTransport } from "@/test-support/api";
import { protocolVersion } from "./jsonRpcSocket";
import { encodeDescriptorCall } from "./descriptorRpc";
import { canonicalBoardFilter } from "./workflowBoardFilters";
import {
  workflowBoundaryGraphIDs as boundaryGraphIDs,
  workflowGraphDraft,
  workflowGraphDraftIDs,
  workflowDefinitionResponse,
  workflowValidationResponse,
  workflowLinksResponse,
  workflowDeletePreviewResponse,
  workflowDeleteResponse,
  workflowGraphSaveImpactResponse,
} from "./clientWorkflowGraph.testFixtures";
const startTaskParamsSchema = z.object({
  task_id: z.literal("task-1"),
  setup_operation_id: z.string(),
});
const appliedStartResponse = {
  outcome: "applied",
  applied: {
    current_nodes: [{ node_id: "node-1", transition_branch_key: null, session_id: null }],
  },
} as const;
describe("ApiClient", () => {
  it("rejects Workflow offsets above the safe-integer ceiling at the binary boundary", () => {
    const method = wf.WorkflowDefinitionService.method.list;
    expect(() =>
      encodeDescriptorCall(method, create(method.input, { offset: 9007199254740992n }), "offset-boundary"),
    ).toThrow();
  });

  it("preserves Workflow pagination cursors through the safe-integer ceiling", async () => {
    const offset = 9007199254740990n;
    const method = wf.WorkflowDefinitionService.method.list;
    const transport = new FakeRpcTransport([
      {
        descriptor: method,
        result: create(method.output, {
          outcome: {
            case: "success",
            value: {
              workflows: [
                {
                  id: "11111111-1111-4111-8111-111111111111",
                  name: "Workflow",
                  version: 1n,
                  executionTargetPolicy: { mode: wf.ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_NONE },
                },
              ],
              nextOffset: offset + 1n,
            },
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    expect((await client.listWorkflows({ offset, limit: 1 })).nextOffset).toBe(offset + 1n);
    expect(transport.descriptorCalls[0]?.request).toMatchObject({ offset, limit: 1 });
  });

  it("parses readiness and sends mutation params through typed method boundary", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: ServerService.method.getReadiness,
        result: create(ServerService.method.getReadiness.output, {
          outcome: {
            case: "success",
            value: {
              readiness: {
                ready: false,
                serverId: "server-1",
                serverVersion: "1.3.0",
                protocolVersion,
                authReady: true,
                endpoint: "ws://127.0.0.1:53082/rpc",
                subagentRoles: [{ name: "default" }, { name: "coder" }],
                causes: [{ code: "unauthenticated", severity: ReadinessSeverity.ERROR }],
              },
            },
          },
        }),
      },
      { method: "workflow.task.start", result: appliedStartResponse },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    const readiness = await client.getReadiness();
    expect(readiness).toMatchObject({
      ready: false,
      serverID: "server-1",
      serverVersion: "1.3.0",
      protocolVersion: protocolVersion,
      subagentRoles: [{ name: "default" }, { name: "coder" }],
      causes: [{ code: "unauthenticated", severity: "error" }],
    });
    expect(readiness.causes[0]).not.toHaveProperty("diagnosticID");
    expect(transport.descriptorCalls[0]?.descriptor).toBe(ServerService.method.getReadiness);
    await expect(client.startTask({ taskID: "task-1" })).resolves.toMatchObject({
      outcome: "applied",
      applied: {
        currentNodes: [{ nodeID: "node-1", transitionBranchKey: null, sessionID: null }],
      },
    });
    const startCall = transport.calls.find((call) => call.method === "workflow.task.start");
    expect(startCall?.options).toEqual({ timeoutMs: null });
    expect(startTaskParamsSchema.parse(startCall?.params).task_id).toBe("task-1");
  });
  it("preserves absent board workflow selectors and normalizes empty slices", async () => {
    const transport = new FakeRpcTransport([
      { method: "workflow.board.get", result: emptyBoardResponse },
      { method: "workflow.board.nodeCards.list", result: emptyBoardNodeCardsResponse },
      { method: "workflow.board.get", result: emptyBoardResponse },
      { method: "workflow.board.nodeCards.list", result: emptyBoardNodeCardsResponse },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(
      client.getBoard(
        "project-1",
        undefined,
        canonicalBoardFilter({ labelFilter: { kind: "none" }, dependencyFilter: null }),
      ),
    ).resolves.toMatchObject({
      projectID: "project-1",
      selectedWorkflow: null,
      workflows: [],
      groups: [],
      columns: [],
    });
    expect(transport.calls).toEqual([
      {
        method: "workflow.board.get",
        params: { project_id: "project-1", label_filter: { kind: "none" }, dependency_filter: null },
      },
    ]);
    const labelID = "f74ce532-9e6e-4cf6-b3c1-d67d5a3eedcf";
    await expect(
      client.listBoardNodeCards({
        projectID: "project-1",
        workflowID: "11111111-1111-4111-8111-111111111111",
        nodeID: "node-1",
        filter: canonicalBoardFilter({
          labelFilter: { kind: "named", mode: "all", labelIDs: [labelID] },
          dependencyFilter: null,
        }),
        offset: 25,
        sort: { field: "labels", direction: "asc" },
      }),
    ).resolves.toMatchObject({
      projectID: "project-1",
      workflowID: "11111111-1111-4111-8111-111111111111",
      nodeID: "node-1",
      cards: [],
      nextOffset: 50,
    });
    expect(transport.calls).toContainEqual({
      method: "workflow.board.nodeCards.list",
      params: {
        project_id: "project-1",
        workflow_id: "11111111-1111-4111-8111-111111111111",
        node_id: "node-1",
        label_filter: { kind: "named", named: { mode: "all", label_ids: [labelID] } },
        dependency_filter: null,
        page_size: 25,
        sort: { field: "labels", direction: "asc" },
        offset: 25,
      },
    });
    const unblockedFilter = canonicalBoardFilter({ labelFilter: { kind: "none" }, dependencyFilter: true });
    await Promise.all([
      client.getBoard("project-1", "11111111-1111-4111-8111-111111111111", unblockedFilter),
      client.listBoardNodeCards({
        projectID: "project-1",
        workflowID: "11111111-1111-4111-8111-111111111111",
        nodeID: "node-1",
        filter: unblockedFilter,
        offset: 0,
        sort: { field: "updated", direction: "desc" },
      }),
    ]);
    expect(transport.calls.slice(2).map(({ method }) => method)).toEqual([
      "workflow.board.get",
      "workflow.board.nodeCards.list",
    ]);
    expect(transport.calls[2]?.params).toMatchObject({ dependency_filter: true });
    expect(transport.calls[3]?.params).toMatchObject({ dependency_filter: true });
  });
  it("rejects malformed Workflow IDs before direct client RPCs or subscriptions", async () => {
    const transport = new FakeRpcTransport([]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    const prefixedID = "workflow-11111111-1111-4111-8111-111111111111";
    await expect(client.getWorkflow(prefixedID)).rejects.toThrow();
    await expect(client.previewWorkflowDelete("not-a-workflow-id")).rejects.toThrow();
    expect(() =>
      client.subscribeWorkflow(prefixedID, {
        onEvent: () => undefined,
        onComplete: () => undefined,
        onError: () => undefined,
      }),
    ).toThrow();
    expect(transport.calls).toEqual([]);
    expect(transport.subscriptions).toEqual([]);
  });
  it("hides workflow join nodes from board columns and groups", async () => {
    const client = new ApiClient(
      new FakeRpcTransport([{ method: "workflow.board.get", result: boardWithJoinResponse }]),
      unexpectedProjectOverflow,
    );
    await expect(
      client.getBoard(
        "project-1",
        "11111111-1111-4111-8111-111111111111",
        canonicalBoardFilter({ labelFilter: { kind: "none" }, dependencyFilter: null }),
      ),
    ).resolves.toMatchObject({
      groups: [{ id: boundaryGraphIDs.nodeGroup, nodeIDs: [boundaryGraphIDs.node] }],
      columns: [{ id: boundaryGraphIDs.node, kind: "agent" }],
    });
  });
  it("parses required empty current task execution arrays", async () => {
    const client = new ApiClient(
      new FakeRpcTransport([{ method: "workflow.task.get", result: emptyTaskDetailResponse }]),
      unexpectedProjectOverflow,
    );
    await expect(client.getTask("task-1")).resolves.toMatchObject({
      id: "task-1",
      labelIDs: ["f74ce532-9e6e-4cf6-b3c1-d67d5a3eedcf"],
      currentNodes: [],
      liveSessions: [],
      currentScripts: [],
      attentionCount: 0,
      sourceURL: "",
    });
  });
  it("uses separate global and task attention RPC contracts", async () => {
    const transport = new FakeRpcTransport([
      {
        method: "workflow.attention.list",
        result: { items: [], next_page_token: "", generated_at_unix_ms: 1 },
      },
      {
        method: "workflow.task.attention.list",
        result: { items: [], generated_at_unix_ms: 2 },
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(client.listAttention("cursor-1")).resolves.toMatchObject({ items: [], nextPageToken: "" });
    await expect(client.listTaskAttention("task-1")).resolves.toMatchObject({ items: [], generatedAt: 2 });
    expect(transport.calls).toEqual([
      {
        method: "workflow.attention.list",
        params: { page_size: 40, page_token: "cursor-1" },
      },
      {
        method: "workflow.task.attention.list",
        params: { task_id: "task-1" },
      },
    ]);
  });
  it("parses task source URL into sourceURL", async () => {
    const client = new ApiClient(
      new FakeRpcTransport([
        {
          method: "workflow.task.get",
          result: {
            task: {
              ...emptyTaskDetailResponse.task,
              source_url: "https://github.com/respawn-llc/kent/issues/1",
            },
          },
        },
      ]),
      unexpectedProjectOverflow,
    );
    await expect(client.getTask("task-1")).resolves.toMatchObject({
      sourceURL: "https://github.com/respawn-llc/kent/issues/1",
    });
  });
  it("maps workflow definition, execution validation, and active project links for the editor", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: wf.WorkflowDefinitionService.method.get,
        result: create(wf.WorkflowDefinitionService.method.get.output, {
          outcome: {
            case: "success",
            value: workflowDefinitionResponse,
          },
        }),
      },
      {
        descriptor: wf.WorkflowDefinitionService.method.validate,
        result: create(wf.WorkflowDefinitionService.method.validate.output, {
          outcome: {
            case: "success",
            value: workflowValidationResponse,
          },
        }),
      },
      {
        descriptor: wf.ProjectLinkService.method.list,
        result: create(wf.ProjectLinkService.method.list.output, {
          outcome: {
            case: "success",
            value: workflowLinksResponse,
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    const definition = await client.getWorkflow("11111111-1111-4111-8111-111111111111");
    expect(definition).toMatchObject({
      derivedWiring: {
        edges: [
          {
            edgeID: boundaryGraphIDs.edge,
            inputBindings: [{ field: "summary", name: "summary", source: "transition_output" }],
            requiredProvisionFields: [{ description: "Summary", name: "summary" }],
            assigneeSelectionApplicability: { available: true, reason: "eligible" },
            thinkingSelectionApplicability: { available: true, reason: "eligible" },
          },
        ],
      },
      workflow: { id: "11111111-1111-4111-8111-111111111111", name: "Delivery", version: 9 },
      nodeGroups: [{ id: boundaryGraphIDs.nodeGroup, key: "core", name: "Core", nodeIDs: [] }],
      transitionGroups: [
        {
          description: "Choose this when implementation is complete.",
          id: boundaryGraphIDs.transitionGroup,
          sourceNodeID: boundaryGraphIDs.node,
          transitionID: "done",
        },
      ],
      edges: [
        {
          contextSource: { kind: "selected_node", nodeKey: "implement" },
          id: boundaryGraphIDs.edge,
          assigneeSelection: "configured",
          thinkingSelection: "configured",
          parameters: [{ description: "Summary", key: "summary", purpose: "ordinary" }],
          promptTemplate: "Summarize the implementation.",
          targetNodeID: boundaryGraphIDs.doneNode,
          transitionGroupID: boundaryGraphIDs.transitionGroup,
        },
      ],
    });
    expect(definition.nodes).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          groupID: boundaryGraphIDs.nodeGroup,
          id: boundaryGraphIDs.node,
          name: "Implement",
          subagentRole: "coder",
        }),
        expect.objectContaining({ groupID: null, id: boundaryGraphIDs.doneNode, name: "Done" }),
      ]),
    );
    await expect(
      client.validateWorkflow("11111111-1111-4111-8111-111111111111", "execution"),
    ).resolves.toMatchObject({
      valid: false,
      errors: [
        {
          code: "workflow.validation.invalid_node_kind",
          workflowID: "11111111-1111-4111-8111-111111111111",
          nodeID: boundaryGraphIDs.node,
          transitionGroupID: boundaryGraphIDs.transitionGroup,
          edgeID: boundaryGraphIDs.edge,
          details: {
            fieldName: "",
            inputName: "summary",
            placeholder: ".Params.summary",
            providerEdgeID: null,
          },
          relatedIDs: [boundaryGraphIDs.relatedEdge],
          blocksContext: true,
        },
      ],
    });
    await expect(client.listProjectWorkflowLinks("project-1")).resolves.toEqual([
      {
        id: "link-1",
        projectID: "project-1",
        workflowID: "11111111-1111-4111-8111-111111111111",
        isDefault: true,
      },
    ]);
    expect(transport.descriptorCalls).toContainEqual({
      descriptor: wf.WorkflowDefinitionService.method.get,
      request: create(wf.WorkflowDefinitionService.method.get.input, {
        workflowId: "11111111-1111-4111-8111-111111111111",
      }),
    });
    expect(transport.descriptorCalls).toContainEqual({
      descriptor: wf.WorkflowDefinitionService.method.validate,
      request: create(wf.WorkflowDefinitionService.method.validate.input, {
        workflowId: "11111111-1111-4111-8111-111111111111",
        mode: wf.ValidationMode.WORKFLOW_VALIDATION_MODE_EXECUTION,
      }),
    });
    expect(transport.descriptorCalls).toContainEqual({
      descriptor: wf.ProjectLinkService.method.list,
      request: create(wf.ProjectLinkService.method.list.input, {
        projectId: "project-1",
      }),
    });
  });
  it("maps previous-target-or-new workflow context sources", async () => {
    const response = {
      definition: {
        ...workflowDefinitionResponse.definition,
        edges: workflowDefinitionResponse.definition.edges.map((edge) => ({
          ...edge,
          contextSource: { kind: wf.ContextSourceKind.WORKFLOW_CONTEXT_SOURCE_KIND_PREVIOUS_TARGET_OR_NEW },
        })),
      },
    };
    const transport = new FakeRpcTransport([
      {
        descriptor: wf.WorkflowDefinitionService.method.get,
        result: create(wf.WorkflowDefinitionService.method.get.output, {
          outcome: {
            case: "success",
            value: response,
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(client.getWorkflow("11111111-1111-4111-8111-111111111111")).resolves.toMatchObject({
      edges: [
        {
          contextSource: { kind: "previous_target_or_new", nodeKey: "" },
          id: boundaryGraphIDs.edge,
        },
      ],
    });
  });
  it("maps workflow library list, create, link, and project create-link contracts", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: wf.WorkflowDefinitionService.method.list,
        result: create(wf.WorkflowDefinitionService.method.list.output, {
          outcome: {
            case: "success",
            value: {
              projectId: "project-1",
              workflows: [
                {
                  id: "11111111-1111-4111-8111-111111111111",
                  name: "Delivery",
                  description: "Ship",
                  version: 4n,
                  executionTargetPolicy: {
                    mode: wf.ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_CUSTOM_REF,
                    customRef: "release/v1",
                  },
                  projectLink: {
                    default: true,
                  },
                },
              ],
              nextOffset: 10n,
            },
          },
        }),
      },
      {
        descriptor: wf.WorkflowDefinitionService.method.create,
        result: create(wf.WorkflowDefinitionService.method.create.output, {
          outcome: {
            case: "success",
            value: {
              workflow: {
                id: "22222222-2222-4222-8222-222222222222",
                name: "Ops",
                description: "",
                version: 1n,
                executionTargetPolicy: {
                  mode: wf.ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_ASK_ON_FIRST_EXECUTION,
                },
              },
            },
          },
        }),
      },
      {
        descriptor: wf.WorkflowDefinitionService.method.createAndLinkProject,
        result: create(wf.WorkflowDefinitionService.method.createAndLinkProject.output, {
          outcome: {
            case: "success",
            value: {
              workflow: {
                id: "33333333-3333-4333-8333-333333333333",
                name: "Project workflow",
                description: "",
                version: 1n,
                executionTargetPolicy: {
                  mode: wf.ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_NONE,
                },
              },
              link: {
                id: "link-3",
                projectId: "project-1",
                workflowId: "33333333-3333-4333-8333-333333333333",
                default: true,
              },
            },
          },
        }),
      },
      {
        descriptor: wf.ProjectLinkService.method.link,
        result: create(wf.ProjectLinkService.method.link.output, {
          outcome: {
            case: "success",
            value: {
              link: {
                id: "link-1",
                projectId: "project-1",
                workflowId: "11111111-1111-4111-8111-111111111111",
                default: false,
              },
            },
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(
      client.listWorkflows({ offset: 0n, limit: 10, projectID: "project-1", query: "ship" }),
    ).resolves.toMatchObject({
      nextOffset: 10n,
      workflows: [
        {
          id: "11111111-1111-4111-8111-111111111111",
          name: "Delivery",
          version: 4,
          executionTargetPolicy: { mode: "custom_ref", customRef: "release/v1" },
          projectLink: { isDefault: true },
        },
      ],
    });
    await expect(client.createWorkflow({ name: "Ops", description: "" })).resolves.toMatchObject({
      id: "22222222-2222-4222-8222-222222222222",
      name: "Ops",
      executionTargetPolicy: { mode: "ask_on_first_execution", customRef: null },
    });
    await expect(
      client.createAndLinkWorkflowToProject({
        projectID: "project-1",
        name: "Project workflow",
        description: "",
      }),
    ).resolves.toMatchObject({
      link: { isDefault: true, projectID: "project-1", workflowID: "33333333-3333-4333-8333-333333333333" },
      workflow: { id: "33333333-3333-4333-8333-333333333333" },
    });
    await expect(
      client.linkWorkflowToProject({
        projectID: "project-1",
        workflowID: "11111111-1111-4111-8111-111111111111",
      }),
    ).resolves.toMatchObject({
      id: "link-1",
      isDefault: false,
    });
    expect(transport.descriptorCalls).toContainEqual({
      descriptor: wf.WorkflowDefinitionService.method.list,
      request: create(wf.WorkflowDefinitionService.method.list.input, {
        offset: 0n,
        limit: 10,
        projectId: "project-1",
        query: "ship",
      }),
    });
    expect(transport.descriptorCalls).toContainEqual({
      descriptor: wf.WorkflowDefinitionService.method.createAndLinkProject,
      request: create(wf.WorkflowDefinitionService.method.createAndLinkProject.input, {
        name: "Project workflow",
        description: "",
        projectId: "project-1",
        defaultPolicy: wf.ProjectLinkDefaultMode.WORKFLOW_PROJECT_LINK_DEFAULT_MODE_IF_PROJECT_HAS_NONE,
      }),
    });
    expect(transport.descriptorCalls).toContainEqual({
      descriptor: wf.ProjectLinkService.method.link,
      request: create(wf.ProjectLinkService.method.link.input, {
        projectId: "project-1",
        workflowId: "11111111-1111-4111-8111-111111111111",
        defaultPolicy: wf.ProjectLinkDefaultMode.WORKFLOW_PROJECT_LINK_DEFAULT_MODE_IF_PROJECT_HAS_NONE,
      }),
    });
  });
  it("maps workflow delete preview and confirmed delete contracts", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: wf.WorkflowDefinitionService.method.deletePreview,
        result: create(wf.WorkflowDefinitionService.method.deletePreview.output, {
          outcome: {
            case: "success",
            value: workflowDeletePreviewResponse,
          },
        }),
      },
      {
        descriptor: wf.WorkflowDefinitionService.method.delete,
        result: create(wf.WorkflowDefinitionService.method.delete.output, {
          outcome: {
            case: "success",
            value: workflowDeleteResponse,
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(client.previewWorkflowDelete("11111111-1111-4111-8111-111111111111")).resolves.toMatchObject(
      {
        workflowID: "11111111-1111-4111-8111-111111111111",
        version: 7,
        projectCount: 1,
        linkCount: 1,
        defaultReplacementProjectCount: 0,
        taskCount: 2,
        currentNodeCount: 0,
        pendingApprovalCount: 1,
        blockedTaskCount: 1,
      },
    );
    await expect(
      client.deleteWorkflow({
        workflowID: "11111111-1111-4111-8111-111111111111",
        confirmed: true,
        expectedVersion: 7,
        expectedProjectCount: 1,
        expectedLinkCount: 1,
        expectedTaskCount: 2,
        cleanupArtifacts: false,
      }),
    ).resolves.toMatchObject({
      deleted: false,
      blockers: [{ code: "pending_approvals", count: 1 }],
    });
    expect(transport.descriptorCalls).toContainEqual({
      descriptor: wf.WorkflowDefinitionService.method.deletePreview,
      request: create(wf.WorkflowDefinitionService.method.deletePreview.input, {
        workflowId: "11111111-1111-4111-8111-111111111111",
      }),
    });
    expect(transport.descriptorCalls).toContainEqual({
      descriptor: wf.WorkflowDefinitionService.method.delete,
      request: create(wf.WorkflowDefinitionService.method.delete.input, {
        workflowId: "11111111-1111-4111-8111-111111111111",
        confirmed: true,
        expectedVersion: 7n,
        expectedProjectCount: 1n,
        expectedLinkCount: 1n,
        expectedTaskCount: 2n,
        cleanupArtifacts: false,
      }),
    });
  });
  it("maps workflow graph draft validation, preview, and save contracts", async () => {
    const graphValidationResults = [
      {
        mode: wf.ValidationMode.WORKFLOW_VALIDATION_MODE_DRAFT,
        result: {
          valid: true,
          errors: [],
        },
      },
      {
        mode: wf.ValidationMode.WORKFLOW_VALIDATION_MODE_EXECUTION,
        result: workflowValidationResponse,
      },
    ];
    const transport = new FakeRpcTransport([
      {
        descriptor: wf.WorkflowGraphService.method.validateDraft,
        result: create(wf.WorkflowGraphService.method.validateDraft.output, {
          outcome: {
            case: "success",
            value: {
              results: graphValidationResults,
              derivedWiring: {
                edges: [
                  {
                    edgeId: workflowGraphDraftIDs.startEdge,
                    inputBindings: [
                      {
                        name: "brief",
                        source: "transition_output",
                        field: "brief",
                      },
                    ],
                    requiredProvisionFields: [
                      {
                        name: "brief",
                        description: "Brief",
                      },
                    ],
                    assigneeSelectionApplicability: {
                      available: true,
                      parameterVisible: true,
                      reason: wf.SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_ELIGIBLE,
                    },
                    thinkingSelectionApplicability: {
                      available: true,
                      parameterVisible: true,
                      reason: wf.SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_ELIGIBLE,
                    },
                  },
                ],
              },
            },
          },
        }),
      },
      {
        descriptor: wf.WorkflowGraphService.method.savePreview,
        result: create(wf.WorkflowGraphService.method.savePreview.output, {
          outcome: {
            case: "success",
            value: {
              changed: true,
              currentVersion: 11n,
              validationResults: graphValidationResults,
              impact: workflowGraphSaveImpactResponse,
              blockers: [
                {
                  code: "confirmation_required",
                  message: "Confirm removal.",
                  count: 1n,
                  affectedEntities: [
                    {
                      entityType: wf.GraphEntityType.WORKFLOW_GRAPH_ENTITY_TYPE_EDGE,
                      entityId: workflowGraphDraftIDs.startEdge,
                    },
                  ],
                },
              ],
              canSave: false,
              confirmationRequired: true,
            },
          },
        }),
      },
      {
        descriptor: wf.WorkflowGraphService.method.save,
        result: create(wf.WorkflowGraphService.method.save.output, {
          outcome: {
            case: "success",
            value: {
              saved: true,
              changed: true,
              definition: workflowDefinitionResponse.definition,
              currentVersion: 12n,
              validationResults: graphValidationResults,
              impact: {
                ...workflowGraphSaveImpactResponse,
                removedNodeGroupCount: 0n,
                removedEdgeCount: 0n,
                removedEntities: [],
              },
              blockers: [],
              canSave: true,
              confirmationRequired: false,
            },
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(
      client.validateWorkflowGraphDraft({
        workflowID: "11111111-1111-4111-8111-111111111111",
        metadata: {
          name: "Draft Workflow",
          description: "Draft description",
          executionTargetPolicy: { mode: "custom_ref", customRef: "release/v1" },
        },
        graph: workflowGraphDraft,
        modes: ["draft", "execution"],
      }),
    ).resolves.toMatchObject({
      draft: { valid: true },
      execution: { valid: false },
      derivedWiring: {
        edges: [
          {
            edgeID: workflowGraphDraftIDs.startEdge,
            inputBindings: [{ field: "brief", name: "brief", source: "transition_output" }],
            requiredProvisionFields: [{ description: "Brief", name: "brief" }],
          },
        ],
      },
    });
    await expect(
      client.previewWorkflowGraphSave({
        workflowID: "11111111-1111-4111-8111-111111111111",
        expectedVersion: 11,
        metadata: {
          name: "Preview Workflow",
          description: "Preview description",
          executionTargetPolicy: { mode: "default_branch", customRef: null },
        },
        graph: workflowGraphDraft,
      }),
    ).resolves.toMatchObject({
      changed: true,
      currentVersion: 11,
      confirmationRequired: true,
      impact: { removedEdgeCount: 1 },
      blockers: [{ code: "confirmation_required" }],
    });
    await expect(
      client.saveWorkflowGraph({
        workflowID: "11111111-1111-4111-8111-111111111111",
        expectedVersion: 11,
        metadata: {
          name: "Saved Workflow",
          description: "Saved description",
          executionTargetPolicy: { mode: "none", customRef: null },
        },
        graph: workflowGraphDraft,
        confirmation: {
          expectedRemovedNodeGroupCount: 1,
          expectedRemovedNodeCount: 0,
          expectedRemovedTransitionGroupCount: 0,
          expectedRemovedEdgeCount: 1,
          expectedNodeTaskReferenceCount: 0,
          expectedEdgeTaskReferenceCount: 0,
        },
      }),
    ).resolves.toMatchObject({
      saved: true,
      changed: true,
      currentVersion: 12,
      definition: { workflow: { id: "11111111-1111-4111-8111-111111111111" } },
      blockers: [],
    });
    expect(transport.descriptorCalls[0]).toEqual({
      descriptor: wf.WorkflowGraphService.method.validateDraft,
      request: create(wf.WorkflowGraphService.method.validateDraft.input, {
        workflowId: "11111111-1111-4111-8111-111111111111",
        metadata: {
          name: "Draft Workflow",
          description: "Draft description",
          executionTargetPolicy: {
            mode: wf.ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_CUSTOM_REF,
            customRef: "release/v1",
          },
        },
        modes: [
          wf.ValidationMode.WORKFLOW_VALIDATION_MODE_DRAFT,
          wf.ValidationMode.WORKFLOW_VALIDATION_MODE_EXECUTION,
        ],
        graph: {
          nodeGroups: [],
          nodes: [
            {
              id: workflowGraphDraftIDs.startNode,
              key: "backlog",
              kind: wf.NodeKind.WORKFLOW_NODE_KIND_START,
              displayName: "Backlog",
              groupId: undefined,
              joinInputProviders: [],
            },
          ],
          transitionGroups: [
            {
              id: workflowGraphDraftIDs.startTransitionGroup,
              sourceNodeId: workflowGraphDraftIDs.startNode,
              transitionId: "start",
              displayName: "Start",
              description: "Start the workflow.",
            },
          ],
          edges: [
            {
              id: workflowGraphDraftIDs.startEdge,
              transitionGroupId: workflowGraphDraftIDs.startTransitionGroup,
              key: "start",
              targetNodeId: workflowGraphDraftIDs.agentNode,
              assigneeSelection: wf.AssigneeSelection.WORKFLOW_ASSIGNEE_SELECTION_CONFIGURED,
              thinkingSelection: wf.ThinkingSelection.WORKFLOW_THINKING_SELECTION_CONFIGURED,
              requiresApproval: false,
              contextMode: wf.ContextMode.WORKFLOW_CONTEXT_MODE_NEW_SESSION,
              contextSource: {
                kind: wf.ContextSourceKind.WORKFLOW_CONTEXT_SOURCE_KIND_IMMEDIATE_SOURCE,
                nodeKey: undefined,
              },
              parameters: [
                {
                  description: "Brief",
                  key: "brief",
                  purpose: wf.ParameterPurpose.WORKFLOW_PARAMETER_PURPOSE_ORDINARY,
                },
              ],
              promptTemplate: "Start from {{.TaskTitle}}.",
            },
          ],
        },
      }),
    });
    expect(transport.descriptorCalls[2]).toMatchObject({
      descriptor: wf.WorkflowGraphService.method.save,
      request: {
        expectedVersion: 11n,
        metadata: {
          name: "Saved Workflow",
          description: "Saved description",
          executionTargetPolicy: {
            mode: wf.ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_NONE,
          },
        },
        confirmation: {
          expectedRemovedNodeGroupCount: 1n,
          expectedRemovedEdgeCount: 1n,
        },
      },
    });
  });
});
const emptyBoardResponse = {
  board: {
    project_id: "project-1",
    project: {
      project_key: "proj",
      display_name: "Project",
      default_workspace_id: "workspace-1",
      attached_workspace_count: 1,
    },
    workflows: null,
    groups: null,
    columns: null,
    generated_at_unix_ms: 1,
  },
};
const boardWithJoinResponse = {
  board: {
    ...emptyBoardResponse.board,
    selected_workflow: {
      workflow_id: "11111111-1111-4111-8111-111111111111",
      display_name: "Workflow",
      description: "",
      version: 1,
      is_project_default: true,
      valid_for_task_creation: true,
      validation_errors: [],
    },
    groups: [
      {
        group_id: boundaryGraphIDs.nodeGroup,
        key: "review",
        display_name: "Review",
        sort_order: 1,
        node_ids: [boundaryGraphIDs.node, boundaryGraphIDs.joinNode],
      },
      {
        group_id: boundaryGraphIDs.joinOnlyNodeGroup,
        key: "join_only",
        display_name: "Join Only",
        sort_order: 2,
        node_ids: [boundaryGraphIDs.joinNode],
      },
    ],
    columns: [
      boardColumnResponse(boundaryGraphIDs.node, "agent"),
      boardColumnResponse(boundaryGraphIDs.joinNode, "join"),
    ],
  },
};
function boardColumnResponse(nodeID: string, kind: string) {
  return {
    node: {
      node_id: nodeID,
      key: nodeID,
      kind,
      display_name: nodeID,
      assignee_role: "",
      output_fields: [],
    },
    group_id: boundaryGraphIDs.nodeGroup,
    sort_order: 1,
    is_backlog: false,
    is_done: false,
    task_count: 0,
  };
}
const emptyBoardNodeCardsResponse = {
  project_id: "project-1",
  workflow_id: "11111111-1111-4111-8111-111111111111",
  node_id: "node-1",
  cards: null,
  next_offset: 50,
  generated_at_unix_ms: 1,
};
const workspaceResponse = {
  workspace_id: "workspace-1",
  display_name: "Project",
  root_path: "/tmp/project",
  availability: "available",
  is_primary: true,
  updated_at_unix_ms: 1,
};
const emptyTaskDetailResponse = {
  task: {
    summary: {
      id: "task-1",
      project_id: "project-1",
      workflow_id: "11111111-1111-4111-8111-111111111111",
      short_id: "PROJ-1",
      title: "Task",
      created_at_unix_ms: 1,
      updated_at_unix_ms: 1,
      done: false,
    },
    project: {
      display_name: "Project",
    },
    workflow: {
      workflow_id: "11111111-1111-4111-8111-111111111111",
      display_name: "Delivery",
      description: "",
      version: 1,
      is_project_default: true,
      valid_for_task_creation: true,
      validation_errors: null,
    },
    body: "Body",
    source_workspace: workspaceResponse,
    status: {
      kind: "backlog",
      native_state: "active",
      node_ids: [],
      attention_types: [],
    },
    actions: {
      can_start: true,
      can_interrupt: false,
      can_resume: false,
      can_delete: true,
    },
    label_ids: ["f74ce532-9e6e-4cf6-b3c1-d67d5a3eedcf"],
    attention_count: 0,
    dependencies: {
      blocker_count: 0,
      unsatisfied_blocker_count: 0,
      directly_blocked_task_count: 0,
      directions: [
        {
          direction: "blocked-by",
          total_count: 0,
          unsatisfied_count: 0,
          items: [],
          add_availability: { available: { remaining_capacity: 5 } },
        },
        {
          direction: "blocks",
          total_count: 0,
          items: [],
          add_availability: { available: { remaining_capacity: 4 } },
        },
      ],
    },
    worktree_path: null,
    current_nodes: [],
    live_sessions: [],
    current_scripts: [],
    retained_session_count: 0,
  },
};
