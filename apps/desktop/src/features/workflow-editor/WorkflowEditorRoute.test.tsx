/* eslint-disable max-lines -- Workflow editor route test fixtures are intentionally colocated with route scenarios. */
import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";

import { App } from "../../App";
import { AppProviders } from "../../app/AppProviders";
import { queryKeys } from "../../app/queryKeys";
import { SidebarProvider } from "../../app/sidebarProvider";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";
import { WorkflowEditorRoute } from "./WorkflowEditorRoute";
import { WorkflowInspectorSidebar } from "./WorkflowInspectorSidebar";
import { WorkflowEditorDraftBridgeProvider } from "./workflowEditorDraftBridge";
import { useWorkflowEditorDraftController } from "./workflowEditorDraftBridgeCore";

describe("WorkflowEditorRoute", () => {
  const originalResizeObserver = globalThis.ResizeObserver;

  beforeEach(() => {
    globalThis.ResizeObserver = MockResizeObserver;
  });

  afterEach(() => {
    globalThis.ResizeObserver = originalResizeObserver;
    vi.restoreAllMocks();
  });

  it("renders a linked workflow graph with the shared validation issue island", async () => {
    window.history.pushState(null, "", "/projects/project-1/workflows/workflow-1/editor");
    render(
      <App
        services={createTestServices([
          ...startupRoutes,
          { method: "workflow.listProjectLinks", result: activeLinkResponse },
          { method: "workflow.board.get", result: boardResponse },
          { method: "workflow.get", result: workflowDefinitionResponse },
          { method: "workflow.validate", result: invalidValidationResponse },
          { method: "workflow.graph.validateDraft", result: graphValidationResponse },
        ])}
      />,
    );

    expect(
      await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 }),
    ).toBeInTheDocument();
    expect(window.location.pathname).toBe("/workflows/workflow-1/editor");
    expect(window.location.search).toContain("projectId=project-1");
    expect(await screen.findAllByTestId("workflow-node-source-handle")).toHaveLength(3);
    expect(await screen.findAllByTestId("workflow-node-target-handle")).toHaveLength(3);
    const issues = await screen.findByRole("complementary", { name: "Workflow issues" });
    expect(within(issues).getAllByText("Done transition is invalid.").length).toBeGreaterThan(0);
    expect(screen.getByTestId("route-transition-frame")).not.toHaveClass("p-[var(--space-2)]");
  });

  it("opens read-only inspectors for workflow metadata and graph entities", async () => {
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(
      <App
        services={createTestServices([
          ...startupRoutes,
          { method: "workflow.get", result: workflowDefinitionResponse },
          { method: "workflow.validate", result: invalidValidationResponse },
          { method: "workflow.graph.validateDraft", result: graphValidationResponse },
        ])}
      />,
    );

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });

    fireEvent.click(screen.getByRole("button", { name: "Inspect workflow" }));
    expect(await screen.findByRole("complementary", { name: "Inspect workflow" })).toHaveTextContent(
      "Graph revision",
    );

    fireEvent.click(screen.getByText("Implement"));
    const nodeInspector = await screen.findByRole("complementary", { name: "Inspect node" });
    expect(within(nodeInspector).getByDisplayValue("coder")).toBeInTheDocument();

    const coreLabels = screen.getAllByText("Core");
    expect(coreLabels.length).toBeGreaterThan(0);
    const coreLabel = coreLabels[0];
    if (coreLabel === undefined) {
      throw new Error("Expected at least one Core label");
    }
    fireEvent.click(coreLabel);
    expect(await screen.findByRole("complementary", { name: "Inspect group" })).toHaveTextContent("Members");

    fireEvent.click(screen.getByTestId("workflow-join-diamond"));
    expect(await screen.findByRole("complementary", { name: "Inspect node" })).toHaveTextContent("join");
  });

  it("renders read-only edge inspector details from cached workflow data", async () => {
    render(
      <AppProviders services={createTestServices(startupRoutes)}>
        <CachedEdgeInspectorFixture />
      </AppProviders>,
    );

    expect(await screen.findByText("Context mode")).toBeInTheDocument();
    expect(screen.getByText("Compact and continue session")).toBeInTheDocument();
    expect(screen.getByText("Node: implement")).toBeInTheDocument();
  });

  it("blocks direct access to workflows not linked to the project", async () => {
    window.history.pushState(null, "", "/workflows/workflow-2/editor?projectId=project-1");
    render(
      <App
        services={createTestServices([
          ...startupRoutes,
          { method: "workflow.listProjectLinks", result: activeLinkResponse },
          { method: "workflow.board.get", result: boardResponse },
        ])}
      />,
    );

    expect(await screen.findByText("Workflow is not linked to this project")).toBeInTheDocument();
    expect(screen.queryByTestId("workflow-editor-canvas")).not.toBeInTheDocument();
  });

  it("opens an unlinked workflow in global editor mode", async () => {
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(
      <App
        services={createTestServices([
          ...startupRoutes,
          { method: "workflow.get", result: workflowDefinitionResponse },
          { method: "workflow.validate", result: { valid: true, errors: [] } },
          {
            method: "workflow.graph.validateDraft",
            result: {
              results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
            },
          },
        ])}
      />,
    );

    expect(
      await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("route-transition-frame")).not.toHaveClass("p-[var(--space-2)]");
  });

  it("shows and acknowledges a dirty-draft conflict when the remote definition revision changes", async () => {
    const services = createTestServices([
      ...startupRoutes,
      {
        method: "workflow.get",
        handler: (_params, callIndex) =>
          workflowDefinitionResponseWithRevision(callIndex <= 0 ? 1 : callIndex + 1),
      },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      {
        method: "workflow.graph.validateDraft",
        result: {
          results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
        },
      },
    ]);
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <WorkflowConflictDriver />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    expect(
      await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 }),
    ).toBeInTheDocument();
    expect(await screen.findByText("Workflow settings changed")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Simulate remote update" }));
    expect(
      await screen.findByText("This workflow changed remotely while you were editing."),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    await waitFor(() => {
      expect(
        screen.queryByText("This workflow changed remotely while you were editing."),
      ).not.toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: "Simulate remote update" }));
    expect(
      await screen.findByText("This workflow changed remotely while you were editing."),
    ).toBeInTheDocument();
  });

  it("allows metadata-only save when draft graph validation is invalid", async () => {
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponse },
      { method: "workflow.validate", result: invalidValidationResponse },
      { method: "workflow.graph.validateDraft", result: graphValidationResponse },
      {
        method: "workflow.graph.savePreview",
        result: {
          current_graph_revision: 1,
          current_definition_revision: 1,
          validation_results: graphValidationResponse.results,
          impact: graphSaveImpactResponse,
          blockers: [],
          can_save: true,
          confirmation_required: false,
        },
      },
      {
        method: "workflow.graph.save",
        result: {
          saved: true,
          definition: workflowDefinitionResponseWithRevision(2).definition,
          current_graph_revision: 1,
          current_definition_revision: 2,
          validation_results: graphValidationResponse.results,
          impact: graphSaveImpactResponse,
          blockers: [],
          can_save: true,
          confirmation_required: false,
        },
      },
    ]);
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <WorkflowMetadataEditDriver />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    expect(
      await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 }),
    ).toBeInTheDocument();
    expect(await screen.findByText("Workflow settings changed")).toBeInTheDocument();

    const saveButton = screen.getByRole("button", { name: "Save" });
    expect(saveButton).toBeEnabled();
    fireEvent.click(saveButton);

    await waitFor(() => {
      const saveCall = services.transport.calls.find((call) => call.method === "workflow.graph.save");
      expect(saveCall?.params).toMatchObject({
        expected_definition_revision: 1,
        metadata: { name: "Locally edited delivery", description: "" },
        workflow_id: "workflow-1",
      });
    });
  });
});

class MockResizeObserver implements ResizeObserver {
  observe(): void {
    return;
  }

  unobserve(): void {
    return;
  }

  disconnect(): void {
    return;
  }
}

function CachedEdgeInspectorFixture() {
  const queryClient = useQueryClient();
  useEffect(() => {
    queryClient.setQueryData(queryKeys.workflowDefinition("workflow-1"), cachedWorkflowDefinition);
    queryClient.setQueryData(queryKeys.workflowValidation("workflow-1", "execution"), cachedValidation);
  }, [queryClient]);
  return <WorkflowInspectorSidebar selection={{ kind: "edge", edgeID: "edge-2" }} workflowID="workflow-1" />;
}

function WorkflowConflictDriver() {
  const queryClient = useQueryClient();
  useOneShotWorkflowMetadataEdit();
  return (
    <button
      onClick={() => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.workflowDefinition("workflow-1") });
      }}
      type="button"
    >
      Simulate remote update
    </button>
  );
}

function WorkflowMetadataEditDriver() {
  useOneShotWorkflowMetadataEdit();
  return null;
}

function useOneShotWorkflowMetadataEdit() {
  const edited = useRef(false);
  const controller = useWorkflowEditorDraftController("workflow-1");
  useEffect(() => {
    if (controller === null || edited.current || controller.state.source.workflow.name.length === 0) {
      return;
    }
    edited.current = true;
    controller.dispatch({
      description: controller.draft.workflow.description,
      name: "Locally edited delivery",
      type: "editWorkflowMetadata",
    });
  }, [controller]);
}

const activeLinkResponse = {
  links: [
    {
      id: "link-1",
      project_id: "project-1",
      workflow_id: "workflow-1",
      default: true,
    },
  ],
};

const workflow = {
  workflow_id: "workflow-1",
  display_name: "Delivery",
  description: "",
  graph_revision: 1,
  is_project_default: true,
  valid_for_task_creation: true,
  validation_errors: [],
};

const boardResponse = {
  board: {
    project_id: "project-1",
    project: { project_key: "PROJ", display_name: "Project" },
    selected_workflow: workflow,
    workflows: [workflow],
    groups: [],
    columns: [],
    generated_at_unix_ms: 1,
  },
};

const workflowDefinitionResponse = {
  definition: {
    workflow: {
      id: "workflow-1",
      name: "Delivery",
      description: "",
      graph_revision: 1,
      definition_revision: 1,
    },
    node_groups: [
      {
        group_id: "group-1",
        workflow_id: "workflow-1",
        group_key: "core",
        display_name: "Core",
        sort_order: 1,
        node_ids: ["node-1"],
      },
    ],
    nodes: [
      {
        id: "node-1",
        workflow_id: "workflow-1",
        key: "implement",
        kind: "agent",
        display_name: "Implement",
        group_id: "group-1",
        group_key: "core",
        subagent_role: "coder",
        prompt_template: "Implement the task.",
        output_fields: [{ name: "summary", description: "Summary" }],
      },
      {
        id: "join",
        workflow_id: "workflow-1",
        key: "join",
        kind: "join",
        display_name: "Join",
      },
      {
        id: "done",
        workflow_id: "workflow-1",
        key: "done",
        kind: "terminal",
        display_name: "Done",
      },
    ],
    transition_groups: [
      {
        id: "tg-1",
        workflow_id: "workflow-1",
        source_node_id: "node-1",
        transition_id: "join",
        display_name: "Join",
      },
      {
        id: "tg-2",
        workflow_id: "workflow-1",
        source_node_id: "join",
        transition_id: "done",
        display_name: "Done",
      },
    ],
    edges: [
      {
        id: "edge-1",
        workflow_id: "workflow-1",
        transition_group_id: "tg-1",
        key: "done",
        target_node_id: "join",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
      },
      {
        id: "edge-2",
        workflow_id: "workflow-1",
        transition_group_id: "tg-2",
        key: "done",
        target_node_id: "done",
        requires_approval: true,
        context_mode: "compact_and_continue_session",
        context_source: { kind: "selected_node", node_key: "implement" },
      },
    ],
  },
};

function workflowDefinitionResponseWithRevision(definitionRevision: number) {
  return {
    definition: {
      ...workflowDefinitionResponse.definition,
      workflow: {
        ...workflowDefinitionResponse.definition.workflow,
        definition_revision: definitionRevision,
      },
    },
  };
}

const invalidValidationResponse = {
  valid: false,
  errors: [
    {
      code: "workflow.validation.invalid",
      message: "Done transition is invalid.",
      workflow_id: "workflow-1",
      node_id: "node-1",
      transition_group_id: "tg-1",
      edge_id: "edge-1",
      related_ids: [],
      blocks_context: true,
    },
  ],
};

const graphValidationResponse = {
  results: {
    draft: invalidValidationResponse,
    execution: invalidValidationResponse,
  },
};

const graphSaveImpactResponse = {
  removed_node_count: 0,
  removed_transition_group_count: 0,
  removed_edge_count: 0,
  node_task_reference_count: 0,
  edge_task_reference_count: 0,
  active_node_placement_count: 0,
  pending_approval_count: 0,
  active_run_count: 0,
  runnable_run_count: 0,
  start_node_change_count: 0,
  last_terminal_change_count: 0,
  task_referenced_node_kind_change_count: 0,
};

const cachedWorkflowDefinition = {
  workflow: { id: "workflow-1", name: "Delivery", description: "", graphRevision: 1, definitionRevision: 1 },
  nodeGroups: [
    { id: "group-1", workflowID: "workflow-1", key: "core", name: "Core", sortOrder: 1, nodeIDs: ["node-1"] },
  ],
  nodes: [
    {
      id: "node-1",
      workflowID: "workflow-1",
      key: "implement",
      kind: "agent",
      name: "Implement",
      groupID: "group-1",
      groupKey: "core",
      subagentRole: "coder",
      promptTemplate: "Implement the task.",
      outputFields: [{ name: "summary", description: "Summary" }],
    },
    {
      id: "join",
      workflowID: "workflow-1",
      key: "join",
      kind: "join",
      name: "Join",
      groupID: "",
      groupKey: "",
      subagentRole: "",
      promptTemplate: "",
      outputFields: [],
    },
    {
      id: "done",
      workflowID: "workflow-1",
      key: "done",
      kind: "terminal",
      name: "Done",
      groupID: "",
      groupKey: "",
      subagentRole: "",
      promptTemplate: "",
      outputFields: [],
    },
  ],
  transitionGroups: [
    { id: "tg-1", workflowID: "workflow-1", sourceNodeID: "node-1", transitionID: "join", name: "Join" },
    { id: "tg-2", workflowID: "workflow-1", sourceNodeID: "join", transitionID: "done", name: "Done" },
  ],
  edges: [
    {
      id: "edge-1",
      workflowID: "workflow-1",
      transitionGroupID: "tg-1",
      key: "done",
      targetNodeID: "join",
      requiresApproval: false,
      contextMode: "new_session",
      contextSource: { kind: "immediate_source", nodeKey: "" },
      inputBindings: [],
      outputRequirements: [],
    },
    {
      id: "edge-2",
      workflowID: "workflow-1",
      transitionGroupID: "tg-2",
      key: "done",
      targetNodeID: "done",
      requiresApproval: true,
      contextMode: "compact_and_continue_session",
      contextSource: { kind: "selected_node", nodeKey: "implement" },
      inputBindings: [],
      outputRequirements: [],
    },
  ],
};

const cachedValidation = {
  valid: false,
  errors: [
    {
      code: "workflow.validation.invalid",
      message: "Done transition is invalid.",
      workflowID: "workflow-1",
      nodeID: "node-1",
      transitionGroupID: "tg-1",
      edgeID: "edge-1",
      relatedIDs: [],
      blocksContext: true,
    },
  ],
};
