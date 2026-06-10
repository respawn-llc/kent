/* eslint-disable max-lines -- Workflow editor route test fixtures are intentionally colocated with route scenarios. */
import {
  createBrowserNativeBridge,
  type NativeBridge,
  type NativeDialogWindowOptions,
  type NativeWorkflowDeleted,
  type NativeWorkflowGraphDeleteConfirmation,
} from "@builder/desktop-native-bridge";
import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, vi } from "vitest";

import { App } from "../../App";
import { AppProviders } from "../../app/AppProviders";
import { queryKeys } from "../../app/queryKeys";
import { SidebarHost } from "../../app/sidebar";
import { useSidebar } from "../../app/sidebarContext";
import { SidebarProvider } from "../../app/sidebarProvider";
import { protocolVersion } from "../../api/jsonRpcSocket";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";
import { WorkflowEditorRoute } from "./WorkflowEditorRoute";
import { WorkflowGraphEdge } from "./WorkflowGraphEdge";
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
    const user = userEvent.setup();
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
    expect(await screen.findAllByTestId("workflow-node-source-handle")).toHaveLength(2);
    expect(screen.queryAllByTestId("workflow-node-target-handle")).toHaveLength(0);
    expect(await screen.findAllByTestId("workflow-node-endpoint-handle")).toHaveLength(4);
    const issues = await screen.findByRole("complementary", { name: "Workflow issues" });
    expect(within(issues).getAllByRole("list").length).toBeGreaterThan(0);
    expect(within(issues).getAllByRole("listitem")).toHaveLength(1);
    expect(within(issues).getByText("Done transition is invalid.")).toBeInTheDocument();
    const legend = screen.getByRole("complementary", { name: "Legend" });

    await user.click(within(legend).getByRole("button", { name: "Expand" }));
    expect(within(legend).getByText("Continue session")).toBeInTheDocument();
    expect(within(legend).getByText("New session")).toBeInTheDocument();
    expect(within(legend).getByText("Multi-agent join")).toBeInTheDocument();
  });

  it("opens inspectors for workflow metadata and graph entities", async () => {
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    const copied: string[] = [];
    render(
      <App
        services={createTestServices(
          [
            ...startupRoutes,
            { method: "workflow.get", result: workflowDefinitionResponse },
            { method: "workflow.validate", result: invalidValidationResponse },
            { method: "workflow.graph.validateDraft", result: graphValidationResponse },
          ],
          nativeBridgeWithClipboard(copied),
        )}
      />,
    );

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });

    fireEvent.click(screen.getByRole("button", { name: "Inspect workflow" }));
    expect(await screen.findByRole("complementary", { name: "Inspect workflow" })).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("workflow-graph-node-node-1"));
    const nodeInspector = await screen.findByRole("complementary", { name: "Inspect node" });
    expect(within(nodeInspector).getByRole("heading", { name: "Inspect node" })).toBeInTheDocument();
    const nodeIDButton = within(nodeInspector).getByRole("button", { name: "Copy node ID node-1" });
    fireEvent.click(nodeIDButton);
    await waitFor(() => {
      expect(copied).toEqual(["node-1"]);
    });
    const assignee = within(nodeInspector).getByRole("button", { name: "Assignee" });
    fireEvent.pointerDown(assignee);
    fireEvent.click(await screen.findByRole("menuitemradio", { name: "reviewer" }));

    fireEvent.click(screen.getByTestId("workflow-graph-group-group-1"));
    expect(await screen.findByRole("complementary", { name: "Inspect group" })).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("workflow-join-diamond"));
    expect(await screen.findByRole("complementary", { name: "Inspect node" })).toBeInTheDocument();
  });

  it("opens workflow delete confirmation in a native dialog window from workflow settings", async () => {
    const opened: NativeDialogWindowOptions[] = [];
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    const services = createTestServices(
      [
        ...startupRoutes,
        { method: "workflow.get", result: workflowDefinitionResponse },
        { method: "workflow.validate", result: invalidValidationResponse },
        { method: "workflow.graph.validateDraft", result: graphValidationResponse },
        { method: "workflow.deletePreview", result: workflowDeletePreviewResponse },
      ],
      nativeWorkflowEntityDeleteDialogBridge(opened),
    );

    render(<App services={services} />);

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.click(screen.getByRole("button", { name: "Inspect workflow" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect workflow" });
    fireEvent.click(within(inspector).getByRole("button", { name: "Delete workflow" }));

    await waitFor(() => {
      expect(opened).toHaveLength(1);
    });
    expect(opened[0]).toMatchObject({
      initialHeight: 300,
      initialWidth: 460,
      route: "/native-dialog/workflow-delete",
      title: "Delete workflow?",
      params: {
        active_run_count: "0",
        blocked_task_count: "0",
        default_replacement_project_count: "0",
        link_count: "1",
        project_count: "1",
        runnable_run_count: "0",
        task_count: "2",
        version: "1",
        workflow_id: "workflow-1",
      },
    });
    expect(screen.queryByRole("dialog", { name: "Delete workflow?" })).not.toBeInTheDocument();
  });

  it("does not redirect the main window after a workflow delete notification for a route the user left", async () => {
    const opened: NativeDialogWindowOptions[] = [];
    const nativeBridge = nativeWorkflowEntityDeleteDialogBridge(opened);
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    const services = createTestServices(
      [
        ...startupRoutes,
        { method: "workflow.get", result: workflowDefinitionResponse },
        { method: "workflow.validate", result: invalidValidationResponse },
        { method: "workflow.graph.validateDraft", result: graphValidationResponse },
        { method: "workflow.deletePreview", result: workflowDeletePreviewResponse },
      ],
      nativeBridge,
    );

    render(<App services={services} />);

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.click(screen.getByRole("button", { name: "Inspect workflow" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect workflow" });
    fireEvent.click(within(inspector).getByRole("button", { name: "Delete workflow" }));
    await waitFor(() => {
      expect(opened).toHaveLength(1);
    });

    fireEvent.click(screen.getByRole("link", { name: "Home" }));
    await screen.findByTestId("home-route-root");
    await act(async () => {
      await nativeBridge.workflowDeletion.notifyDeleted({ workflowID: "workflow-1" });
    });

    expect(window.location.pathname).toBe("/");
    expect(
      await within(screen.getByTestId("sonner-test-surface")).findByText("Workflow deleted"),
    ).toBeInTheDocument();
  });

  it("submits workflow delete only once from the browser fallback dialog", async () => {
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponse },
      { method: "workflow.validate", result: invalidValidationResponse },
      { method: "workflow.graph.validateDraft", result: graphValidationResponse },
      { method: "workflow.deletePreview", result: workflowDeletePreviewResponse },
      { method: "workflow.delete", result: workflowDeleteResponse },
      { method: "workflow.list", result: { workflows: [], next_page_token: "" } },
    ]);

    render(<App services={services} />);

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.click(screen.getByRole("button", { name: "Inspect workflow" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect workflow" });
    fireEvent.click(within(inspector).getByRole("button", { name: "Delete workflow" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete workflow?" });
    const confirm = within(dialog).getByRole("button", { name: "Delete workflow" });

    fireEvent.click(confirm);
    fireEvent.click(confirm);

    await waitFor(() => {
      expect(services.transport.calls.filter((call) => call.method === "workflow.delete")).toHaveLength(1);
    });
    expect(
      await within(screen.getByTestId("sonner-test-surface")).findByText("Workflow deleted"),
    ).toBeInTheDocument();
  });

  it("deletes a workflow from the native workflow delete dialog route", async () => {
    let closeCount = 0;
    const deleted: NativeWorkflowDeleted[] = [];
    window.history.pushState(
      null,
      "",
      "/native-dialog/workflow-delete?workflow_id=workflow-1&version=1&project_count=1&link_count=1&task_count=2&default_replacement_project_count=0&active_run_count=0&runnable_run_count=0&blocked_task_count=0",
    );
    const services = createTestServices(
      [
        ...startupRoutes,
        {
          method: "workflow.delete",
          result: workflowDeleteResponse,
        },
      ],
      nativeWorkflowDeleteWindowBridge(
        () => {
          closeCount += 1;
        },
        (event) => {
          deleted.push(event);
        },
      ),
    );

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "Delete workflow" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.delete",
        params: {
          workflow_id: "workflow-1",
          confirmed: true,
          expected_version: 1,
          expected_project_count: 1,
          expected_link_count: 1,
          expected_task_count: 2,
        },
      });
    });
    expect(services.transport.calls.map((call) => call.method)).toContain("server.readiness.get");
    expect(deleted).toEqual([{ workflowID: "workflow-1" }]);
    expect(closeCount).toBe(1);
  });

  it("submits workflow delete only once from the native workflow delete dialog route", async () => {
    let closeCount = 0;
    let resolveDelete: ((value: typeof workflowDeleteResponse) => void) | undefined;
    window.history.pushState(
      null,
      "",
      "/native-dialog/workflow-delete?workflow_id=workflow-1&version=1&project_count=1&link_count=1&task_count=2&default_replacement_project_count=0&active_run_count=0&runnable_run_count=0&blocked_task_count=0",
    );
    const services = createTestServices(
      [
        ...startupRoutes,
        {
          method: "workflow.delete",
          async handler() {
            const response = await new Promise((resolve) => {
              resolveDelete = resolve;
            });
            return response;
          },
        },
      ],
      nativeWorkflowDeleteWindowBridge(
        () => {
          closeCount += 1;
        },
        () => undefined,
      ),
    );

    render(<App services={services} />);

    const confirm = await screen.findByRole("button", { name: "Delete workflow" });
    fireEvent.click(confirm);
    fireEvent.click(confirm);

    expect(services.transport.calls.filter((call) => call.method === "workflow.delete")).toHaveLength(1);
    await act(async () => {
      resolveDelete?.(workflowDeleteResponse);
    });
    await waitFor(() => {
      expect(closeCount).toBe(1);
    });
  });

  it("does not offer workflow delete retry when native notification fails after commit", async () => {
    let closeCount = 0;
    window.history.pushState(
      null,
      "",
      "/native-dialog/workflow-delete?workflow_id=workflow-1&version=1&project_count=1&link_count=1&task_count=2&default_replacement_project_count=0&active_run_count=0&runnable_run_count=0&blocked_task_count=0",
    );
    const services = createTestServices(
      [
        ...startupRoutes,
        {
          method: "workflow.delete",
          result: workflowDeleteResponse,
        },
      ],
      nativeWorkflowDeleteWindowBridge(
        () => {
          closeCount += 1;
        },
        () => {
          throw new Error("event bus down");
        },
      ),
    );

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "Delete workflow" }));

    expect(services.transport.calls.filter((call) => call.method === "workflow.delete")).toHaveLength(1);
    const dialog = screen.getByRole("dialog", { name: "Delete workflow?" });
    expect(
      await within(dialog).findByText(
        "Workflow was deleted, but the main window could not be updated: event bus down",
      ),
    ).toBeInTheDocument();
    expect(within(dialog).queryByRole("button", { name: "Delete workflow" })).not.toBeInTheDocument();

    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
    expect(closeCount).toBe(1);
  });

  it("does not delete a workflow from a malformed native workflow delete dialog route", async () => {
    window.history.pushState(
      null,
      "",
      "/native-dialog/workflow-delete?workflow_id=workflow-1&version=bad&project_count=1&link_count=1&task_count=2&default_replacement_project_count=0&active_run_count=0&runnable_run_count=0&blocked_task_count=0",
    );
    const services = createTestServices(
      startupRoutes,
      nativeWorkflowDeleteWindowBridge(
        () => undefined,
        () => undefined,
      ),
    );

    render(<App services={services} />);

    expect(await screen.findByRole("dialog", { name: "Invalid native dialog" })).toBeInTheDocument();
    expect(services.transport.calls.map((call) => call.method)).not.toContain("workflow.delete");
  });

  it("closes an inspected node sidebar when the node is deleted from its context menu", async () => {
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

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.pointerEnter(screen.getByRole("button", { name: "Add node" }));
    fireEvent.click(await screen.findByRole("button", { name: "Agent node" }));

    expect(await within(canvas).findByText("New agent")).toBeInTheDocument();
    fireEvent.click(within(canvas).getByText("New agent"));
    expect(await screen.findByRole("complementary", { name: "Inspect node" })).toBeInTheDocument();
    fireEvent.contextMenu(within(canvas).getByText("New agent"));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Delete node" }));

    await waitFor(() => {
      expect(within(canvas).queryByText("New agent")).not.toBeInTheDocument();
      expect(screen.queryByRole("complementary", { name: "Inspect node" })).not.toBeInTheDocument();
    });
  });

  it("confirms local cascade deletes in a modal dialog before removing connected graph rows", async () => {
    const user = userEvent.setup();
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponse },
      { method: "workflow.validate", result: invalidValidationResponse },
      { method: "workflow.graph.validateDraft", result: graphValidationResponse },
    ]);
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(<App services={services} />);

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    const readinessCallsBeforeDelete = services.transport.calls.filter(
      (call) => call.method === "server.readiness.get",
    ).length;
    fireEvent.click(within(canvas).getByText("Implement"));
    fireEvent.keyDown(window, { key: "Delete" });

    const confirmation = await screen.findByRole("dialog", { name: "Delete node?" });
    expect(screen.queryByRole("complementary", { name: "Delete node?" })).not.toBeInTheDocument();
    expect(services.transport.calls.filter((call) => call.method === "server.readiness.get")).toHaveLength(
      readinessCallsBeforeDelete,
    );
    expect(within(confirmation).getByText("Nodes: 1")).toBeInTheDocument();
    expect(
      within(confirmation).getByText("This will remove connected transition branches and their parameters."),
    ).toBeInTheDocument();
    expect(within(confirmation).getByText("Branches: 1")).toBeInTheDocument();
    expect(within(confirmation).getByText("Transition groups: 1")).toBeInTheDocument();
    expect(within(canvas).getByText("Implement")).toBeInTheDocument();
    await waitFor(() => {
      expect(within(confirmation).getByRole("button", { name: "Close" })).toHaveFocus();
    });
    await user.tab();
    expect(within(confirmation).getByRole("button", { name: "Cancel" })).toHaveFocus();
    await user.tab();
    expect(within(confirmation).getByRole("button", { name: "Delete node" })).toHaveFocus();
    await user.tab();
    expect(within(confirmation).getByRole("button", { name: "Close" })).toHaveFocus();

    await user.keyboard("{Escape}");
    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: "Delete node?" })).not.toBeInTheDocument();
    });
    expect(within(canvas).getByText("Implement")).toBeInTheDocument();

    fireEvent.click(within(canvas).getByText("Implement"));
    fireEvent.keyDown(window, { key: "Delete" });
    const reopenedConfirmation = await screen.findByRole("dialog", { name: "Delete node?" });
    fireEvent.click(within(reopenedConfirmation).getByRole("button", { name: "Delete node" }));

    await waitFor(() => {
      expect(within(canvas).queryByText("Implement")).not.toBeInTheDocument();
    });
  });

  it("deletes an edge from its right-click menu through the draft graph flow", async () => {
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponse },
      { method: "workflow.validate", result: invalidValidationResponse },
      { method: "workflow.graph.validateDraft", result: graphValidationResponse },
    ]);
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <EdgeContextMenuDeleteDriver />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    expect(screen.getByTestId("edge-delete-driver-edges")).toHaveTextContent("edge-1,edge-2");
    fireEvent.contextMenu(screen.getByTestId("workflow-edge-hit-path"));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Delete branch" }));

    await waitFor(() => {
      expect(screen.getByTestId("edge-delete-driver-edges")).toHaveTextContent("edge-1");
    });
  });

  it("confirms prompt text loss during cascading node deletion", async () => {
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponseWithReviewBranch },
      { method: "workflow.validate", result: invalidValidationResponse },
      { method: "workflow.graph.validateDraft", result: graphValidationResponse },
    ]);
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(<App services={services} />);

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.click(within(canvas).getByTestId("workflow-graph-node-node-1"));
    fireEvent.keyDown(window, { key: "Delete" });

    const confirmation = await screen.findByRole("dialog", { name: "Delete node?" });
    expect(
      within(confirmation).getByText("This will remove prompt text from deleted transition branches."),
    ).toBeInTheDocument();
    expect(within(confirmation).getByText("Prompts with text: 1")).toBeInTheDocument();
    expect(within(canvas).getByTestId("workflow-graph-node-node-1")).toBeInTheDocument();
  });

  it("repairs separate grouped branch transitions through delete drag save flow", async () => {
    const destructiveImpact = {
      ...graphSaveImpactResponse,
      removed_edge_count: 1,
    };
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponseWithSeparateGroupedBranchTransitions },
      { method: "workflow.validate", result: { valid: false, errors: [] } },
      {
        method: "workflow.graph.validateDraft",
        handler(params: unknown) {
          return hasRepairedGroupedBranchFanout(params)
            ? validGraphValidationResponse
            : invalidNodeGroupValidationResponse;
        },
      },
      {
        method: "workflow.graph.savePreview",
        result: {
          current_version: 1,
          validation_results: validGraphValidationResponse.results,
          impact: destructiveImpact,
          blockers: [{ code: "confirmation_required", message: "Confirm removal.", count: 1 }],
          can_save: true,
          confirmation_required: true,
        },
      },
      {
        method: "workflow.graph.save",
        result: {
          saved: true,
          definition: workflowDefinitionResponseWithSeparateGroupedBranchTransitions.definition,
          current_version: 2,
          validation_results: validGraphValidationResponse.results,
          impact: destructiveImpact,
          blockers: [],
          can_save: true,
          confirmation_required: false,
        },
      },
    ]);
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <WorkflowFanoutProbe />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    expect(screen.getByTestId("workflow-fanout-probe")).toHaveTextContent("tg-plan-new-agent,tg-plan-implement");

    fireEvent.click(screen.getByRole("button", { name: "Delete stale branch edge" }));
    await waitFor(() => {
      expect(screen.getByTestId("workflow-fanout-probe")).toHaveTextContent("tg-plan-new-agent");
    });

    fireEvent.click(screen.getByRole("button", { name: "Drag Plan to Implement" }));
    await waitFor(() => {
      expect(screen.getByTestId("workflow-fanout-probe")).toHaveTextContent("tg-plan-new-agent,tg-plan-new-agent");
    });
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
    });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(
      await screen.findByText("This save will permanently remove graph rows. Proceed?"),
    ).toBeInTheDocument();
    expect(screen.getByText("Removed branches: 1")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Confirm save" }));

    await waitFor(() => {
      expect(services.transport.calls.some((call) => call.method === "workflow.graph.save")).toBe(true);
    });
    const saveCall = services.transport.calls.find((call) => call.method === "workflow.graph.save");
    const expectedEdges: unknown = expect.arrayContaining([
      expect.objectContaining({ target_node_id: "impl-a", transition_group_id: "tg-plan-new-agent" }),
      expect.objectContaining({ target_node_id: "impl-b", transition_group_id: "tg-plan-new-agent" }),
    ]);
    const expectedConfirmation: unknown = expect.objectContaining({
      expected_removed_edge_count: 1,
    });
    const expectedGraph: unknown = expect.objectContaining({
      edges: expectedEdges,
    });
    const expectedParams: unknown = expect.objectContaining({
      confirmation: expectedConfirmation,
      graph: expectedGraph,
    });
    expect(saveCall?.params).toEqual(expectedParams);
  });

  it("keeps a dragged node in a Start-backed group when automatic topology wiring cannot be inferred", async () => {
    const warning =
      "Node added to the group, but automatic parallel wiring could not be inferred. If the group starts from Backlog, insert a split agent after Backlog; otherwise review the group fan-out and join branches before saving.";
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponseWithStartGroupAndUnrelatedAgent },
      { method: "workflow.validate", result: invalidValidationResponse },
      {
        method: "workflow.graph.validateDraft",
        handler(_params, callIndex) {
          return callIndex === 0 ? validGraphValidationResponse : invalidNodeGroupValidationResponse;
        },
      },
    ]);
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <WorkflowNodeGroupProbe nodeID="review" testID="review-group-probe" />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    expect(screen.getByTestId("review-group-probe")).toHaveTextContent("none");

    const card = screen.getByTestId("workflow-graph-node-review");
    const eventView = card.ownerDocument.defaultView;
    if (eventView === null) {
      throw new Error("Expected test document to have a default window");
    }
    const elementFromPoint = vi.fn<typeof document.elementFromPoint>(() => card);
    const restoreElementFromPoint = mockDocumentElementFromPoint(elementFromPoint);
    try {
      dispatchMouseEvent(card, eventView, "mousedown", { button: 0, clientX: 12, clientY: 18 });
      dispatchMouseEvent(document, eventView, "mousemove", { buttons: 1, clientX: 28, clientY: 34 });
      dispatchMouseEvent(document, eventView, "mousemove", { buttons: 1, clientX: 40, clientY: 48 });
      await waitFor(() => {
        expect(screen.getByTestId("workflow-group-drag-preview")).toHaveTextContent("Review");
      });
      Object.defineProperty(screen.getByTestId("workflow-graph-group-group-1"), "getBoundingClientRect", {
        configurable: true,
        value: () => new eventView.DOMRect(0, 0, 320, 240),
      });
      dispatchMouseEvent(document, eventView, "mouseup", { clientX: 40, clientY: 48 });
      expect(elementFromPoint).toHaveBeenCalledWith(40, 48);
    } finally {
      restoreElementFromPoint();
    }

    await waitFor(() => {
      expect(screen.getByTestId("review-group-probe")).toHaveTextContent("group-1");
    });
    const toastSurface = screen.getByTestId("sonner-test-surface");
    expect(await within(toastSurface).findByText(warning)).toBeInTheDocument();

    fireEvent.click(card);
    expect(within(toastSurface).getAllByText(warning)).toHaveLength(1);

    expect(workflowDraftValidationCallCount(services)).toBe(1);
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => {
      expect(workflowDraftValidationCallCount(services)).toBe(2);
    });
    expect(services.transport.calls.map((call) => call.method)).not.toContain("workflow.graph.savePreview");
  });

  it("confirms drag-out extraction and preserves the extracted branch incoming edge", async () => {
    vi.spyOn(globalThis.crypto, "randomUUID").mockReturnValue("00000000-0000-4000-8000-000000000042");
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponseWithValidGroupedBranches },
      { method: "workflow.validate", result: invalidValidationResponse },
      { method: "workflow.graph.validateDraft", result: validGraphValidationResponse },
    ]);
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <WorkflowNodeGroupProbe nodeID="impl-b" testID="impl-b-group-probe" />
            <WorkflowFanoutProbe />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    expect(screen.getByTestId("impl-b-group-probe")).toHaveTextContent("group-parallel");
    expect(screen.getByTestId("workflow-fanout-probe")).toHaveTextContent("tg-plan-new-agent,tg-plan-new-agent");

    dragWorkflowNodeOut(screen.getByTestId("workflow-graph-node-impl-b"));

    const confirmation = await screen.findByRole("dialog", { name: "Remove node from group?" });
    expect(
      within(confirmation).getByText(
        "This will remove the node from the group and remove group join wiring that no longer applies.",
      ),
    ).toBeInTheDocument();
    expect(within(confirmation).getByText("Nodes: 1")).toBeInTheDocument();
    expect(within(confirmation).getByText("Branches: 2")).toBeInTheDocument();
    expect(within(confirmation).getByText("Transition groups: 2")).toBeInTheDocument();
    expect(screen.getByTestId("impl-b-group-probe")).toHaveTextContent("group-parallel");

    fireEvent.click(within(confirmation).getByRole("button", { name: "Remove from group" }));

    await waitFor(() => {
      expect(screen.getByTestId("impl-b-group-probe")).toHaveTextContent("none");
    });
    expect(screen.getByTestId("workflow-fanout-probe")).toHaveTextContent(
      "tg-plan-new-agent,workflow-transition-group-00000000-0000-4000-8000-000000000042",
    );
  });

  it("opens drag-out extraction confirmation in a native dialog window with extraction copy", async () => {
    vi.spyOn(globalThis.crypto, "randomUUID").mockReturnValue("00000000-0000-4000-8000-000000000044");
    const opened: NativeDialogWindowOptions[] = [];
    const nativeBridge = nativeWorkflowDeleteDialogBridge(opened);
    const services = createTestServices(
      [
        ...startupRoutes,
        { method: "workflow.get", result: workflowDefinitionResponseWithValidGroupedBranches },
        { method: "workflow.validate", result: invalidValidationResponse },
        { method: "workflow.graph.validateDraft", result: validGraphValidationResponse },
      ],
      nativeBridge,
    );
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    dragWorkflowNodeOut(screen.getByTestId("workflow-graph-node-impl-b"));

    await waitFor(() => {
      expect(opened).toHaveLength(1);
    });
    expect(opened[0]).toMatchObject({
      route: "/native-dialog/workflow-delete-confirm",
      title: "Remove node from group?",
      params: {
        edgeCount: "2",
        nodeCount: "1",
        operation: "extract",
        requestID: "workflow-1-delete-1",
        transitionGroupCount: "2",
      },
    });
    expect(screen.queryByRole("dialog", { name: "Remove node from group?" })).not.toBeInTheDocument();
  });

  it("preserves extraction copy when opening the native graph confirmation route", async () => {
    window.history.pushState(
      null,
      "",
      "/native-dialog/workflow-delete-confirm?requestID=workflow-1-delete-1&nodeCount=1&edgeCount=2&transitionGroupCount=2&operation=extract",
    );
    render(<App services={createTestServices(startupRoutes)} />);

    const confirmation = await screen.findByRole("dialog", { name: "Remove node from group?" });
    expect(
      within(confirmation).getByText(
        "This will remove the node from the group and remove group join wiring that no longer applies.",
      ),
    ).toBeInTheDocument();
    expect(within(confirmation).getByRole("button", { name: "Remove from group" })).toBeInTheDocument();
  });

  it("rejects stale drag-out extraction confirmations after draft graph changes", async () => {
    vi.spyOn(globalThis.crypto, "randomUUID").mockReturnValue("00000000-0000-4000-8000-000000000043");
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponseWithValidGroupedBranches },
      { method: "workflow.validate", result: invalidValidationResponse },
      { method: "workflow.graph.validateDraft", result: validGraphValidationResponse },
    ]);
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <WorkflowNodeGroupProbe nodeID="impl-b" testID="impl-b-group-probe" />
            <WorkflowFanoutProbe />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    dragWorkflowNodeOut(screen.getByTestId("workflow-graph-node-impl-b"));

    const confirmation = await screen.findByRole("dialog", { name: "Remove node from group?" });
    fireEvent.click(screen.getByRole("button", { name: "Delete stale branch edge" }));
    fireEvent.click(within(confirmation).getByRole("button", { name: "Remove from group" }));

    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: "Remove node from group?" })).not.toBeInTheDocument();
    });
    expect(screen.getByTestId("impl-b-group-probe")).toHaveTextContent("group-parallel");
    expect(screen.getByTestId("workflow-fanout-probe")).not.toHaveTextContent(
      "workflow-transition-group-00000000-0000-4000-8000-000000000043",
    );
    expect(
      await within(screen.getByTestId("sonner-test-surface")).findByText(
        "The graph changed before confirmation. Review the graph and delete again.",
      ),
    ).toBeInTheDocument();
  });

  it("opens cascade delete confirmation in a native dialog window when available", async () => {
    const opened: NativeDialogWindowOptions[] = [];
    const nativeBridge = nativeWorkflowDeleteDialogBridge(opened);
    const services = createTestServices(
      [
        ...startupRoutes,
        { method: "workflow.get", result: workflowDefinitionResponse },
        { method: "workflow.validate", result: invalidValidationResponse },
        { method: "workflow.graph.validateDraft", result: graphValidationResponse },
      ],
      nativeBridge,
    );
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(<App services={services} />);

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.click(within(canvas).getByText("Implement"));
    fireEvent.keyDown(window, { key: "Delete" });

    await waitFor(() => {
      expect(opened).toHaveLength(1);
    });
    expect(opened[0]).toMatchObject({
      initialHeight: 260,
      initialWidth: 420,
      route: "/native-dialog/workflow-delete-confirm",
      title: "Delete node?",
      params: {
        edgeCount: "1",
        nodeCount: "1",
        transitionGroupCount: "1",
      },
    });
    expect(opened[0]?.params.requestID).toBe("workflow-1-delete-1");
    expect(screen.queryByRole("dialog", { name: "Delete node?" })).not.toBeInTheDocument();
    expect(within(canvas).getByText("Implement")).toBeInTheDocument();

    await act(async () => {
      await nativeBridge.workflowEditor.confirmGraphDelete({ requestID: "workflow-1-delete-1" });
    });

    await waitFor(() => {
      expect(within(canvas).queryByText("Implement")).not.toBeInTheDocument();
    });
  });

  it("rejects stale native graph delete confirmations after draft graph changes", async () => {
    const opened: NativeDialogWindowOptions[] = [];
    const nativeBridge = nativeWorkflowDeleteDialogBridge(opened);
    const services = createTestServices(
      [
        ...startupRoutes,
        { method: "workflow.get", result: workflowDefinitionResponse },
        { method: "workflow.validate", result: invalidValidationResponse },
        { method: "workflow.graph.validateDraft", result: graphValidationResponse },
      ],
      nativeBridge,
    );
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <StaleNativeGraphDeleteDriver />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.click(within(canvas).getByText("Implement"));
    fireEvent.keyDown(window, { key: "Delete" });

    await waitFor(() => {
      expect(opened).toHaveLength(1);
    });
    fireEvent.click(screen.getByRole("button", { name: "Add stale edge" }));
    expect(screen.getByTestId("stale-native-delete-driver-edges")).toHaveTextContent("edge-stale");

    await act(async () => {
      await nativeBridge.workflowEditor.confirmGraphDelete({ requestID: "workflow-1-delete-1" });
    });

    expect(
      await within(screen.getByTestId("sonner-test-surface")).findByText(
        "The graph changed before confirmation. Review the graph and delete again.",
      ),
    ).toBeInTheDocument();
    expect(within(canvas).getByText("Implement")).toBeInTheDocument();
    expect(screen.getByTestId("stale-native-delete-driver-edges")).toHaveTextContent("edge-stale");
  });

  it("rejects stale native graph delete confirmations after deleted prompt count changes", async () => {
    const opened: NativeDialogWindowOptions[] = [];
    const nativeBridge = nativeWorkflowDeleteDialogBridge(opened);
    const services = createTestServices(
      [
        ...startupRoutes,
        { method: "workflow.get", result: workflowDefinitionResponse },
        { method: "workflow.validate", result: invalidValidationResponse },
        { method: "workflow.graph.validateDraft", result: graphValidationResponse },
      ],
      nativeBridge,
    );
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <StalePromptNativeGraphDeleteDriver />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.click(within(canvas).getByText("Implement"));
    fireEvent.keyDown(window, { key: "Delete" });

    await waitFor(() => {
      expect(opened).toHaveLength(1);
    });
    expect(opened[0]?.params.promptCount).toBe("0");
    fireEvent.click(screen.getByRole("button", { name: "Add stale prompt" }));
    expect(screen.getByTestId("stale-native-delete-driver-prompt")).toHaveTextContent("Added stale prompt.");

    await act(async () => {
      await nativeBridge.workflowEditor.confirmGraphDelete({ requestID: "workflow-1-delete-1" });
    });

    expect(
      await within(screen.getByTestId("sonner-test-surface")).findByText(
        "The graph changed before confirmation. Review the graph and delete again.",
      ),
    ).toBeInTheDocument();
    expect(within(canvas).getByText("Implement")).toBeInTheDocument();
  });

  it("shows local feedback when native delete confirmation listener registration fails", async () => {
    const nativeBridge = nativeWorkflowDeleteListenerFailureBridge();
    const services = createTestServices(
      [
        ...startupRoutes,
        { method: "workflow.get", result: workflowDefinitionResponse },
        { method: "workflow.validate", result: invalidValidationResponse },
        { method: "workflow.graph.validateDraft", result: graphValidationResponse },
      ],
      nativeBridge,
    );
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(<App services={services} />);

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });

    expect(
      await within(screen.getByTestId("sonner-test-surface")).findByText(
        "Delete confirmation listener failed: listener unavailable",
      ),
    ).toBeInTheDocument();
  });

  it("shows a toast when initial node deletion is blocked", async () => {
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(
      <App
        services={createTestServices([
          ...startupRoutes,
          { method: "workflow.get", result: workflowDefinitionResponseWithStartNode },
          { method: "workflow.validate", result: invalidValidationResponse },
          { method: "workflow.graph.validateDraft", result: graphValidationResponse },
        ])}
      />,
    );

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.click(within(canvas).getByTestId("workflow-graph-node-start"));
    fireEvent.keyDown(window, { key: "Delete" });

    const toastSurface = screen.getByTestId("sonner-test-surface");
    expect(await within(toastSurface).findByText("The initial node cannot be deleted.")).toBeInTheDocument();
    expect(
      screen
        .queryAllByTestId("floating-notice-header")
        .some((element) => element.textContent === "Delete blocked"),
    ).toBe(false);
    expect(within(canvas).getByTestId("workflow-graph-node-start")).toBeInTheDocument();
  });

  it("shows a toast when last Terminal Node deletion is blocked", async () => {
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

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.click(within(canvas).getByTestId("workflow-graph-node-done"));
    fireEvent.keyDown(window, { key: "Delete" });

    const toastSurface = screen.getByTestId("sonner-test-surface");
    expect(
      await within(toastSurface).findByText("At least one Terminal node must remain in the workflow."),
    ).toBeInTheDocument();
    expect(
      screen
        .queryAllByTestId("floating-notice-header")
        .some((element) => element.textContent === "Delete blocked"),
    ).toBe(false);
    expect(within(canvas).getByTestId("workflow-graph-node-done")).toBeInTheDocument();
  });

  it("keeps a legacy assignee selected when it is missing from configured readiness roles", async () => {
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(
      <App
        services={createTestServices([
          ...startupRoutesWithSubagentRoles(["default", "reviewer"]),
          { method: "workflow.get", result: workflowDefinitionResponseWithAgentRole("legacy_coder") },
          { method: "workflow.validate", result: invalidValidationResponse },
          { method: "workflow.graph.validateDraft", result: graphValidationResponse },
        ])}
      />,
    );

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.click(within(canvas).getByTestId("workflow-graph-node-node-1"));
    const nodeInspector = await screen.findByRole("complementary", { name: "Inspect node" });
    const assignee = within(nodeInspector).getByRole("button", { name: "Assignee" });

    fireEvent.pointerDown(assignee);
    expect(await screen.findByRole("menuitemradio", { name: "legacy_coder" })).toBeInTheDocument();
  });

  it("inserts prompt placeholders from branch parameters and built-in chips", async () => {
    const user = userEvent.setup();
    render(
      <AppProviders
        services={createTestServices([
          ...startupRoutes,
          { method: "workflow.get", result: workflowDefinitionResponseWithReviewBranch },
          { method: "workflow.validate", result: invalidValidationResponse },
          { method: "workflow.graph.validateDraft", result: graphValidationResponse },
        ])}
      >
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <OpenEdgeInspectorButton edgeID="edge-review" />
            <SidebarHost />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    expect(
      await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open edge inspector" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect branch" });
    const prompt = within(inspector).getByRole("textbox", { name: "Prompt" });
    if (!(prompt instanceof HTMLTextAreaElement)) {
      throw new Error("Expected Prompt to render as a textarea.");
    }
    const placeholders = within(inspector).getByRole("group", { name: "Prompt placeholders" });

    expect(
      within(placeholders)
        .getAllByRole("button")
        .map((chip) => chip.textContent),
    ).toEqual([
      ".Params.summary",
      ".TaskId",
      ".TaskShortId",
      ".TaskTitle",
      ".TaskBody",
      ".NodeId",
      ".NodeKey",
      ".NodeDisplayName",
    ]);
    expect(within(placeholders).getByRole("button", { name: ".Params.summary" })).toHaveAttribute(
      "data-placeholder-tone",
      "primary",
    );
    expect(within(placeholders).getByRole("button", { name: ".TaskId" })).toHaveAttribute(
      "data-placeholder-tone",
      "muted",
    );

    prompt.focus();
    prompt.setSelectionRange("Review".length, "Review".length);
    await user.click(within(placeholders).getByRole("button", { name: ".Params.summary" }));
    await waitFor(() => {
      expect(prompt).toHaveValue("Review{{.Params.summary}} the task.");
    });
    expect(prompt.selectionStart).toBe("Review{{.Params.summary}}".length);

    prompt.blur();
    await user.click(within(placeholders).getByRole("button", { name: ".TaskTitle" }));
    await waitFor(() => {
      expect(prompt).toHaveValue("Review{{.Params.summary}} the task.{{.TaskTitle}}");
    });
    expect(prompt).toHaveFocus();
    expect(prompt.selectionStart).toBe("Review{{.Params.summary}} the task.{{.TaskTitle}}".length);
  });

  it("keeps edited branch prompts after saving from the server-returned graph", async () => {
    const user = userEvent.setup();
    const savedPrompt = "Persist this edge prompt.";
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponseWithReviewBranch },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      { method: "workflow.graph.validateDraft", result: validGraphValidationResponse },
      {
        method: "workflow.graph.savePreview",
        result: {
          current_version: 1,
          validation_results: validGraphValidationResponse.results,
          impact: graphSaveImpactResponse,
          blockers: [],
          can_save: true,
          confirmation_required: false,
        },
      },
      {
        method: "workflow.graph.save",
        handler(params) {
          const expectedEdges: unknown = expect.arrayContaining([
            expect.objectContaining({
              id: "edge-review",
              prompt_template: savedPrompt,
            }),
          ]);
          expect(params).toMatchObject({
            graph: {
              edges: expectedEdges,
            },
          });
          return {
            saved: true,
            definition: workflowDefinitionResponseWithEdgePrompt("edge-review", savedPrompt, 2).definition,
            current_version: 2,
            validation_results: validGraphValidationResponse.results,
            impact: graphSaveImpactResponse,
            blockers: [],
            can_save: true,
            confirmation_required: false,
          };
        },
      },
    ]);
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <OpenEdgeInspectorButton edgeID="edge-review" />
            <SidebarHost />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    expect(
      await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open edge inspector" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect branch" });
    const prompt = within(inspector).getByRole("textbox", { name: "Prompt" });

    await user.clear(prompt);
    await user.type(prompt, savedPrompt);
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(prompt).toHaveValue(savedPrompt);
    });
    expect(services.transport.calls.some((call) => call.method === "workflow.graph.save")).toBe(true);
  });

  it("validates empty agent-target prompts at save boundary", async () => {
    const user = userEvent.setup();
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponseWithReviewBranch },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      {
        method: "workflow.graph.validateDraft",
        handler(_params, callIndex) {
          return callIndex === 0
            ? validGraphValidationResponse
            : emptyAgentPromptExecutionGraphValidationResponse;
        },
      },
      {
        method: "workflow.graph.savePreview",
        result: {
          current_version: 1,
          validation_results: emptyAgentPromptExecutionGraphValidationResponse.results,
          impact: graphSaveImpactResponse,
          blockers: [],
          can_save: true,
          confirmation_required: false,
        },
      },
      {
        method: "workflow.graph.save",
        handler(params) {
          const edges = workflowGraphSaveEdges(params);
          const reviewEdge = edges.find((edge) => edge.id === "edge-review");
          expect(reviewEdge).toEqual(expect.objectContaining({ id: "edge-review" }));
          expect(reviewEdge).not.toHaveProperty("prompt_template");
          return {
            saved: true,
            definition: workflowDefinitionResponseWithEdgePrompt("edge-review", "", 2).definition,
            current_version: 2,
            validation_results: emptyAgentPromptExecutionGraphValidationResponse.results,
            impact: graphSaveImpactResponse,
            blockers: [],
            can_save: true,
            confirmation_required: false,
          };
        },
      },
    ]);
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <OpenEdgeInspectorButton edgeID="edge-review" />
            <SidebarHost />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    expect(
      await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 }),
    ).toBeInTheDocument();
    await waitFor(() => {
      expect(workflowDraftValidationCallCount(services)).toBe(1);
    });
    fireEvent.click(screen.getByRole("button", { name: "Open edge inspector" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect branch" });
    const validationCallsBeforeEdit = workflowDraftValidationCallCount(services);

    await user.clear(within(inspector).getByRole("textbox", { name: "Prompt" }));
    expect(workflowDraftValidationCallCount(services)).toBe(validationCallsBeforeEdit);
    const transportCallCountBeforeSave = services.transport.calls.length;

    const unsavedChanges = await screen.findByRole("complementary", { name: "Unsaved changes" });
    await waitFor(() => {
      expect(within(unsavedChanges).getByRole("button", { name: "Save" })).toBeEnabled();
    });

    fireEvent.click(within(unsavedChanges).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(services.transport.calls.some((call) => call.method === "workflow.graph.save")).toBe(true);
    });
    const relevantSaveCalls = services.transport.calls
      .slice(transportCallCountBeforeSave)
      .map((call) => call.method)
      .filter((method) =>
        ["workflow.graph.validateDraft", "workflow.graph.savePreview", "workflow.graph.save"].includes(method),
      );
    expect(relevantSaveCalls.slice(0, 3)).toEqual([
      "workflow.graph.validateDraft",
      "workflow.graph.savePreview",
      "workflow.graph.save",
    ]);
  });

  it("edits Start node display name and key from the normal node inspector", async () => {
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponseWithStartNode },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      {
        method: "workflow.graph.validateDraft",
        result: {
          derived_wiring: graphValidationResponse.derived_wiring,
          results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
        },
      },
      {
        method: "workflow.graph.savePreview",
        result: {
          current_version: 1,
          validation_results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
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
          definition: workflowDefinitionResponseWithStartNode.definition,
          current_version: 2,
          validation_results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
          impact: graphSaveImpactResponse,
          blockers: [],
          can_save: true,
          confirmation_required: false,
        },
      },
    ]);
    render(<App services={services} />);

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.click(within(canvas).getByTestId("workflow-graph-node-start"));
    const nodeInspector = await screen.findByRole("complementary", { name: "Inspect node" });
    const displayName = within(nodeInspector).getByRole("textbox", { name: "Display name" });
    const key = within(nodeInspector).getByRole("textbox", { name: "Key" });

    expect(displayName).toHaveValue("Start");
    expect(key).toHaveValue("start");
    expectIdentifierInputCorrectionsDisabled(key);
    expect(within(nodeInspector).queryByRole("button", { name: "Assignee" })).not.toBeInTheDocument();
    expect(within(nodeInspector).queryByRole("textbox", { name: "Prompt" })).not.toBeInTheDocument();
    expect(
      within(nodeInspector).queryByRole("button", { name: "Add required input" }),
    ).not.toBeInTheDocument();

    fireEvent.change(displayName, { target: { value: "Backlog" } });
    fireEvent.change(key, { target: { value: "backlog" } });

    expect(within(nodeInspector).getByRole("textbox", { name: "Display name" })).toHaveValue("Backlog");
    expect(within(nodeInspector).getByRole("textbox", { name: "Key" })).toHaveValue("backlog");
    await waitFor(() => {
      expect(within(canvas).getByText("Backlog")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => {
      const saveCall = services.transport.calls.find((call) => call.method === "workflow.graph.save");
      const expectedNodes: unknown = expect.arrayContaining([
        expect.objectContaining({
          id: "start",
          key: "backlog",
          display_name: "Backlog",
          kind: "start",
        }),
      ]);
      expect(saveCall?.params).toMatchObject({
        graph: {
          nodes: expectedNodes,
        },
      });
    });
  });

  it("edits newly added Terminal node display name and key from the normal node inspector", async () => {
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponse },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      {
        method: "workflow.graph.validateDraft",
        result: {
          derived_wiring: graphValidationResponse.derived_wiring,
          results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
        },
      },
      {
        method: "workflow.graph.savePreview",
        result: {
          current_version: 1,
          validation_results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
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
          current_version: 2,
          validation_results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
          impact: graphSaveImpactResponse,
          blockers: [],
          can_save: true,
          confirmation_required: false,
        },
      },
    ]);
    render(<App services={services} />);

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.pointerEnter(screen.getByRole("button", { name: "Add node" }));
    fireEvent.click(await screen.findByRole("button", { name: "Terminal node" }));
    fireEvent.click(await within(canvas).findByText("New terminal"));
    const nodeInspector = await screen.findByRole("complementary", { name: "Inspect node" });
    const displayName = within(nodeInspector).getByRole("textbox", { name: "Display name" });
    const key = within(nodeInspector).getByRole("textbox", { name: "Key" });

    expect(displayName).toHaveValue("New terminal");
    expectIdentifierInputCorrectionsDisabled(key);
    expect(within(nodeInspector).queryByRole("button", { name: "Assignee" })).not.toBeInTheDocument();
    expect(within(nodeInspector).queryByRole("textbox", { name: "Prompt" })).not.toBeInTheDocument();
    expect(
      within(nodeInspector).queryByRole("button", { name: "Add required input" }),
    ).not.toBeInTheDocument();

    fireEvent.change(displayName, { target: { value: "Archived" } });
    fireEvent.change(key, { target: { value: "archived" } });

    expect(within(nodeInspector).getByRole("textbox", { name: "Display name" })).toHaveValue("Archived");
    expect(within(nodeInspector).getByRole("textbox", { name: "Key" })).toHaveValue("archived");
    await waitFor(() => {
      expect(within(canvas).getByText("Archived")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => {
      const saveCall = services.transport.calls.find((call) => call.method === "workflow.graph.save");
      const expectedNodes: unknown = expect.arrayContaining([
        expect.objectContaining({
          key: "archived",
          display_name: "Archived",
          kind: "terminal",
        }),
      ]);
      expect(saveCall?.params).toMatchObject({
        graph: {
          nodes: expectedNodes,
        },
      });
    });
  });

  it("edits branch parameters as draggable sidebar islands with top insertion", async () => {
    const user = userEvent.setup();
    mockParameterLayout();
    render(
      <AppProviders
        services={createTestServices([
          ...startupRoutes,
          { method: "workflow.get", result: workflowDefinitionResponse },
          { method: "workflow.validate", result: invalidValidationResponse },
          { method: "workflow.graph.validateDraft", result: graphValidationResponse },
        ])}
      >
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <OpenEdgeInspectorButton edgeID="edge-1" />
            <SidebarHost />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    expect(
      await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open edge inspector" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect branch" });
    const addButton = within(inspector).getByRole("button", { name: "Add parameter" });

    expect(parameterKeys(inspector)).toEqual(["summary"]);
    expect(within(inspector).getByRole("button", { name: "Reorder parameter" })).toBeInTheDocument();
    expect(within(inspector).getByTestId("workflow-parameter")).toBeInTheDocument();

    fireEvent.click(addButton);

    const newParameterKey = within(inspector).getAllByRole("textbox", { name: "Parameter key" })[0];
    const newParameterDescription = within(inspector).getAllByRole("textbox", {
      name: "Parameter description",
    })[0];
    if (newParameterKey === undefined || newParameterDescription === undefined) {
      throw new Error("Expected a new parameter row.");
    }
    expect(parameterKeys(inspector)).toEqual(["", "summary"]);
    expect(newParameterKey).toHaveValue("");
    expectIdentifierInputCorrectionsDisabled(newParameterKey);
    expect(within(inspector).getAllByRole("button", { name: "Reorder parameter" })).toHaveLength(2);
    expect(within(inspector).getAllByTestId("workflow-parameter")).toHaveLength(2);

    newParameterKey.focus();
    for (const character of "details") {
      await user.keyboard(character);
      expect(newParameterKey).toHaveFocus();
    }
    await user.type(newParameterDescription, "Implementation details");
    expect(within(inspector).getAllByRole("textbox", { name: "Parameter key" })[0]).toHaveValue("details");
    expect(parameterKeys(inspector)).toEqual(["details", "summary"]);

    const dragSurfaces = within(inspector).getAllByRole("button", { name: "Reorder parameter" });
    const summaryDragSurface = dragSurfaces[1] ?? dragSurfaces[0];
    summaryDragSurface?.focus();
    expect(summaryDragSurface).toHaveFocus();
    await user.keyboard("[Enter][ArrowUp][Enter]");

    await waitFor(() => {
      expect(parameterKeys(inspector)).toEqual(["summary", "details"]);
    });

    const detailsDeleteButton = within(inspector).getAllByRole("button", { name: "Delete parameter" })[1];
    if (detailsDeleteButton === undefined) {
      throw new Error("Expected a delete button for the reordered parameter.");
    }
    fireEvent.click(detailsDeleteButton);
    await waitFor(() => {
      expect(within(inspector).getAllByTestId("workflow-parameter")).toHaveLength(1);
    });
    expect(parameterKeys(inspector)).toEqual(["summary"]);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("keeps transition parameter name edits local without draft-validation RPC calls", async () => {
    const user = userEvent.setup();
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponse },
      { method: "workflow.validate", result: invalidValidationResponse },
      { method: "workflow.graph.validateDraft", result: graphValidationResponse },
    ]);
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <OpenEdgeInspectorButton edgeID="edge-1" />
            <SidebarHost />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    expect(await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 })).toBeInTheDocument();
    await waitFor(() => {
      expect(workflowDraftValidationCallCount(services)).toBe(1);
    });
    fireEvent.click(screen.getByRole("button", { name: "Open edge inspector" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect branch" });
    const validationCallsBeforeEdit = workflowDraftValidationCallCount(services);

    fireEvent.click(within(inspector).getByRole("button", { name: "Add parameter" }));
    const newParameterKey = within(inspector).getAllByRole("textbox", { name: "Parameter key" })[0];
    if (newParameterKey === undefined) {
      throw new Error("Expected new parameter key field.");
    }
    newParameterKey.focus();
    for (const character of "details") {
      await user.keyboard(character);
      expect(workflowDraftValidationCallCount(services)).toBe(validationCallsBeforeEdit);
    }

    expect(newParameterKey).toHaveValue("details");
    expect(workflowDraftValidationCallCount(services)).toBe(validationCallsBeforeEdit);
    const unsavedChanges = await screen.findByRole("complementary", { name: "Unsaved changes" });
    expect(within(unsavedChanges).getByRole("button", { name: "Save" })).toBeEnabled();
    expect(within(unsavedChanges).queryByText("Done transition is invalid.")).not.toBeInTheDocument();
  });

  it("edits join provider selections from server-derived aggregate parameters", async () => {
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

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.click(within(canvas).getByTestId("workflow-join-diamond"));
    const nodeInspector = await screen.findByRole("complementary", { name: "Inspect node" });
    const providerSelect = within(nodeInspector).getByRole("button", { name: "summary" });

    fireEvent.pointerDown(providerSelect);
    fireEvent.click(await screen.findByRole("menuitemradio", { name: "Implement / done" }));
  });

  it("edits edge route and config facts from the draft inspector", async () => {
    const copied: string[] = [];
    const services = createTestServices(
      [
        ...startupRoutes,
        { method: "workflow.get", result: workflowDefinitionResponse },
        { method: "workflow.validate", result: { valid: true, errors: [] } },
        {
          method: "workflow.graph.validateDraft",
          result: {
            derived_wiring: graphValidationResponse.derived_wiring,
            results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
          },
        },
        {
          method: "workflow.graph.savePreview",
          result: {
            current_version: 1,
            validation_results: {
              draft: { valid: true, errors: [] },
              execution: { valid: true, errors: [] },
            },
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
            current_version: 2,
            validation_results: {
              draft: { valid: true, errors: [] },
              execution: { valid: true, errors: [] },
            },
            impact: graphSaveImpactResponse,
            blockers: [],
            can_save: true,
            confirmation_required: false,
          },
        },
      ],
      nativeBridgeWithClipboard(copied),
    );
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <OpenEdgeInspectorButton />
            <SidebarHost />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    expect(canvas).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open edge inspector" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect branch" });
    const edgeIDButton = within(inspector).getByRole("button", { name: "Copy branch ID edge-2" });
    expect(within(inspector).queryByText("ID")).not.toBeInTheDocument();
    fireEvent.click(edgeIDButton);
    await waitFor(() => {
      expect(copied).toEqual(["edge-2"]);
    });

    fireEvent.change(within(inspector).getByRole("textbox", { name: "Label" }), {
      target: { value: "Review" },
    });
    fireEvent.change(within(inspector).getByRole("textbox", { name: "Model-facing description" }), {
      target: { value: "Choose this when implementation needs review." },
    });
    fireEvent.change(within(inspector).getByRole("textbox", { name: "Key" }), {
      target: { value: "review" },
    });

    const routeSections = within(inspector).getAllByRole("region", { name: "Route" });
    expect(routeSections).toHaveLength(1);
    const routeSection = routeSections[0];
    if (routeSection === undefined) {
      throw new Error("Expected a merged edge route section.");
    }
    expect(within(routeSection).getByRole("textbox", { name: "Label" })).toBeInTheDocument();
    expect(within(routeSection).getByRole("textbox", { name: "Model-facing description" })).toBeInTheDocument();
    expect(within(routeSection).getByRole("textbox", { name: "Key" })).toBeInTheDocument();
    expect(within(routeSection).queryByRole("textbox", { name: "Branch key" })).not.toBeInTheDocument();
    expect(
      within(inspector).queryByRole("region", { name: "Derived parameter bindings" }),
    ).not.toBeInTheDocument();
    expect(
      within(inspector).queryByRole("region", { name: "Derived provision requirements" }),
    ).not.toBeInTheDocument();
    const routeControls = within(routeSection);
    expect(routeControls.queryByRole("button", { name: "Target node" })).not.toBeInTheDocument();
    expect(routeControls.queryByRole("button", { name: "Context mode" })).not.toBeInTheDocument();
    expect(routeControls.queryByRole("button", { name: "Context source" })).not.toBeInTheDocument();
    expect(routeControls.queryByText("Source node")).not.toBeInTheDocument();
    expect(routeControls.queryByText("Target node")).not.toBeInTheDocument();
    const routeGraphic = within(inspector).getByTestId("workflow-edge-route-graphic");
    expect(routeGraphic).toHaveAccessibleName("Join to Done");
    expect(within(routeGraphic).getByTestId("workflow-edge-route-source")).toHaveTextContent("Join");
    expect(within(routeGraphic).getByTestId("workflow-edge-route-target")).toHaveTextContent("Done");
    const requiresApproval = routeControls.getByRole("checkbox", { name: "Requires approval" });
    expect(requiresApproval).toBeChecked();
    expect(routeControls.queryByText("None")).not.toBeInTheDocument();
    expect(routeControls.queryByText("Required")).not.toBeInTheDocument();

    fireEvent.click(requiresApproval);

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      const saveCall = services.transport.calls.find((call) => call.method === "workflow.graph.save");
      const expectedTransitionGroups: unknown = expect.arrayContaining([
        expect.objectContaining({
          id: "tg-2",
          description: "Choose this when implementation needs review.",
          transition_id: "review",
          display_name: "Review",
        }),
      ]);
      const expectedEdges: unknown = expect.arrayContaining([
        expect.objectContaining({
          id: "edge-2",
          key: "done",
          requires_approval: false,
        }),
      ]);
      expect(saveCall?.params).toMatchObject({
        graph: {
          transition_groups: expectedTransitionGroups,
          edges: expectedEdges,
        },
      });
    });
  });

  it("shows branch key only for fan-out transition branches", async () => {
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponseWithValidGroupedBranches },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      { method: "workflow.graph.validateDraft", result: validGraphValidationResponse },
    ]);
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <OpenEdgeInspectorButton edgeID="edge-plan-implement" />
            <SidebarHost />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.click(screen.getByRole("button", { name: "Open edge inspector" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect branch" });
    const routeSection = within(inspector).getByRole("region", { name: "Route" });

    expect(within(routeSection).queryByRole("textbox", { name: "Transition ID" })).not.toBeInTheDocument();
    expect(within(routeSection).getByRole("textbox", { name: "Key" })).toHaveValue("new_agent");
    expect(within(routeSection).getByRole("textbox", { name: "Branch key" })).toHaveValue("implement");
  });

  it("hides context controls for transitions into non-agent nodes", async () => {
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponse },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      { method: "workflow.graph.validateDraft", result: validGraphValidationResponse },
    ]);
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <OpenEdgeInspectorButton edgeID="edge-1" />
            <SidebarHost />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.click(screen.getByRole("button", { name: "Open edge inspector" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect branch" });
    const routeSection = within(inspector).getByRole("region", { name: "Route" });

    expect(within(routeSection).queryByRole("button", { name: "Context mode" })).not.toBeInTheDocument();
    expect(within(routeSection).queryByRole("button", { name: "Context source" })).not.toBeInTheDocument();
    expect(within(routeSection).queryByText("Context mode")).not.toBeInTheDocument();
    expect(within(routeSection).queryByText("Context source")).not.toBeInTheDocument();
  });

  it("disables context source for new-session edges and saves immediate source", async () => {
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponseWithReviewBranch },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      {
        method: "workflow.graph.validateDraft",
        result: {
          derived_wiring: graphValidationResponse.derived_wiring,
          results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
        },
      },
      {
        method: "workflow.graph.savePreview",
        result: {
          current_version: 1,
          validation_results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
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
          current_version: 2,
          validation_results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
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
            <OpenEdgeInspectorButton edgeID="edge-review" />
            <SidebarHost />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    expect(
      await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open edge inspector" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect branch" });

    fireEvent.pointerDown(within(inspector).getByRole("button", { name: "Context mode" }));
    fireEvent.click(await screen.findByRole("menuitemradio", { name: "Continue session" }));
    fireEvent.pointerDown(within(inspector).getByRole("button", { name: "Context mode" }));
    fireEvent.click(await screen.findByRole("menuitemradio", { name: "New session" }));

    const routeSection = within(inspector).getByRole("region", { name: "Route" });
    const contextSource = within(routeSection).getByRole("button", { name: "Context source" });
    expect(contextSource).toBeDisabled();
    expect(within(routeSection).getByText("Immediate source")).toBeInTheDocument();
    fireEvent.change(within(routeSection).getByRole("textbox", { name: "Label" }), {
      target: { value: "Review updated" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      const saveCall = services.transport.calls.find((call) => call.method === "workflow.graph.save");
      const expectedEdges: unknown = expect.arrayContaining([
        expect.objectContaining({
          id: "edge-review",
          context_mode: "new_session",
          context_source: { kind: "immediate_source", node_key: "" },
        }),
      ]);
      expect(saveCall?.params).toMatchObject({
        graph: {
          edges: expectedEdges,
        },
      });
    });
  });

  it("filters context source choices to guaranteed agent predecessors", async () => {
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponseWithStartAndReviewBranch },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      {
        method: "workflow.graph.validateDraft",
        result: {
          derived_wiring: graphValidationResponse.derived_wiring,
          results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
        },
      },
    ]);
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <OpenEdgeInspectorButton edgeID="edge-review" />
            <SidebarHost />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    expect(
      await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open edge inspector" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect branch" });
    const routeSection = within(inspector).getByRole("region", { name: "Route" });

    fireEvent.pointerDown(within(routeSection).getByRole("button", { name: "Context mode" }));
    fireEvent.click(await screen.findByRole("menuitemradio", { name: "Continue session" }));
    fireEvent.pointerDown(within(routeSection).getByRole("button", { name: "Context source" }));

    expect(await screen.findByRole("menuitemradio", { name: "Implement" })).toBeInTheDocument();
    expect(await screen.findByRole("menuitemradio", { name: "Previous run of this target" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    expect(screen.queryByRole("menuitemradio", { name: "Review" })).not.toBeInTheDocument();
    expect(screen.queryByRole("menuitemradio", { name: "Start" })).not.toBeInTheDocument();
    expect(screen.queryByRole("menuitemradio", { name: "Join" })).not.toBeInTheDocument();
    expect(screen.queryByRole("menuitemradio", { name: "Done" })).not.toBeInTheDocument();
  });

  it("selects previous target context source for loop-back transitions", async () => {
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponseWithLoopbackTarget },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      {
        method: "workflow.graph.validateDraft",
        result: {
          derived_wiring: graphValidationResponse.derived_wiring,
          results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
        },
      },
      {
        method: "workflow.graph.savePreview",
        result: {
          current_version: 1,
          validation_results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
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
          current_version: 2,
          validation_results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
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
            <OpenEdgeInspectorButton edgeID="edge-rework" />
            <SidebarHost />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    expect(
      await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open edge inspector" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect branch" });
    const routeSection = within(inspector).getByRole("region", { name: "Route" });

    fireEvent.pointerDown(within(routeSection).getByRole("button", { name: "Context mode" }));
    fireEvent.click(await screen.findByRole("menuitemradio", { name: "Continue session" }));
    fireEvent.pointerDown(within(routeSection).getByRole("button", { name: "Context source" }));
    const previousTarget = await screen.findByRole("menuitemradio", { name: "Previous run of this target" });
    expect(previousTarget).not.toHaveAttribute("aria-disabled", "true");
    fireEvent.click(previousTarget);

    expect(within(routeSection).getByText("Previous run of this target")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      const saveCall = services.transport.calls.find((call) => call.method === "workflow.graph.save");
      const expectedEdges: unknown = expect.arrayContaining([
        expect.objectContaining({
          id: "edge-rework",
          context_mode: "continue_session",
          context_source: { kind: "previous_target", node_key: "" },
        }),
      ]);
      expect(saveCall?.params).toMatchObject({
        graph: {
          edges: expectedEdges,
        },
      });
    });
  });

  it("disables route controls that do not apply to the start edge", async () => {
    const user = userEvent.setup();
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponseWithStartNode },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      {
        method: "workflow.graph.validateDraft",
        result: {
          derived_wiring: graphValidationResponse.derived_wiring,
          results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
        },
      },
    ]);
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <OpenEdgeInspectorButton edgeID="edge-start" />
            <SidebarHost />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    expect(
      await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open edge inspector" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect branch" });
    const routeSection = within(inspector).getByRole("region", { name: "Route" });
    const contextMode = within(routeSection).getByRole("button", { name: "Context mode" });
    const contextSource = within(routeSection).getByRole("button", { name: "Context source" });
    const requiresApproval = within(routeSection).getByRole("checkbox", { name: "Requires approval" });

    expect(contextMode).toBeDisabled();
    expect(contextSource).toBeDisabled();
    expect(requiresApproval).toBeDisabled();

    await user.hover(contextMode);
    expect(await screen.findByRole("tooltip")).toHaveTextContent("N/A for current branch configuration");
    await user.unhover(contextMode);

    fireEvent.pointerDown(contextMode);
    expect(screen.queryByRole("menuitemradio", { name: "Continue session" })).not.toBeInTheDocument();
    fireEvent.pointerDown(contextSource);
    expect(screen.queryByRole("menuitemradio", { name: "Join" })).not.toBeInTheDocument();
    fireEvent.click(requiresApproval);
    expect(requiresApproval).not.toBeChecked();

    within(inspector).getByRole("textbox", { name: "Key" }).focus();
    await user.tab();
    expect(contextMode).not.toHaveFocus();
    expect(contextSource).not.toHaveFocus();
    expect(requiresApproval).not.toHaveFocus();

    fireEvent.keyDown(contextMode, { code: "Enter", key: "Enter" });
    fireEvent.keyDown(contextMode, { code: "Space", key: " " });
    expect(screen.queryByRole("menuitemradio", { name: "Continue session" })).not.toBeInTheDocument();
    fireEvent.keyDown(contextSource, { code: "Enter", key: "Enter" });
    fireEvent.keyDown(contextSource, { code: "Space", key: " " });
    expect(screen.queryByRole("menuitemradio", { name: "Join" })).not.toBeInTheDocument();
    fireEvent.keyDown(requiresApproval, { code: "Enter", key: "Enter" });
    fireEvent.keyDown(requiresApproval, { code: "Space", key: " " });
    expect(requiresApproval).not.toBeChecked();
  });

  it("hides branch parameter authoring for Start-source transitions", async () => {
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponseWithStartNode },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      {
        method: "workflow.graph.validateDraft",
        result: {
          derived_wiring: graphValidationResponse.derived_wiring,
          results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
        },
      },
    ]);
    render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <OpenEdgeInspectorButton edgeID="edge-start" />
            <SidebarHost />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    expect(
      await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open edge inspector" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect branch" });

    expect(within(inspector).getByRole("textbox", { name: "Prompt" })).toBeInTheDocument();
    expect(within(inspector).queryByRole("button", { name: "Add parameter" })).not.toBeInTheDocument();
    expect(within(inspector).queryAllByTestId("workflow-parameter")).toHaveLength(0);
  });

  it("serializes enabled edge approval and reloads it from the saved graph", async () => {
    const savedWorkflowResponse = workflowDefinitionResponseWithEdgeApproval("edge-1", true, 2);
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponse },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      {
        method: "workflow.graph.validateDraft",
        result: {
          derived_wiring: graphValidationResponse.derived_wiring,
          results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
        },
      },
      {
        method: "workflow.graph.savePreview",
        result: {
          current_version: 1,
          validation_results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
          impact: graphSaveImpactResponse,
          blockers: [],
          can_save: true,
          confirmation_required: false,
        },
      },
      {
        method: "workflow.graph.save",
        handler: async () => {
          await new Promise<never>(() => undefined);
        },
      },
    ]);
    const view = render(
      <AppProviders services={services}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <OpenEdgeInspectorButton edgeID="edge-1" />
            <SidebarHost />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    expect(
      await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open edge inspector" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect branch" });
    const routeSection = within(inspector).getByRole("region", { name: "Route" });
    expect(within(routeSection).queryByRole("button", { name: "Context mode" })).not.toBeInTheDocument();
    expect(within(routeSection).queryByRole("button", { name: "Context source" })).not.toBeInTheDocument();
    const requiresApproval = within(routeSection).getByRole("checkbox", { name: "Requires approval" });
    expect(requiresApproval).not.toBeChecked();

    fireEvent.click(requiresApproval);
    expect(requiresApproval).toBeChecked();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      const saveCall = services.transport.calls.find((call) => call.method === "workflow.graph.save");
      const expectedEdges: unknown = expect.arrayContaining([
        expect.objectContaining({
          id: "edge-1",
          requires_approval: true,
        }),
      ]);
      expect(saveCall?.params).toMatchObject({
        graph: {
          edges: expectedEdges,
        },
      });
    });
    view.unmount();

    const reloadServices = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: savedWorkflowResponse },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      {
        method: "workflow.graph.validateDraft",
        result: {
          derived_wiring: graphValidationResponse.derived_wiring,
          results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
        },
      },
    ]);
    render(
      <AppProviders services={reloadServices}>
        <SidebarProvider>
          <WorkflowEditorDraftBridgeProvider>
            <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
            <OpenEdgeInspectorButton edgeID="edge-1" />
            <SidebarHost />
          </WorkflowEditorDraftBridgeProvider>
        </SidebarProvider>
      </AppProviders>,
    );

    expect(
      await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open edge inspector" }));
    const reloadedInspector = await screen.findByRole("complementary", { name: "Inspect branch" });
    const reloadedRouteSection = within(reloadedInspector).getByRole("region", { name: "Route" });

    expect(within(reloadedRouteSection).getByRole("checkbox", { name: "Requires approval" })).toBeChecked();
  });

  it("keeps the graph canvas mounted while workflow metadata fields update", async () => {
    const user = userEvent.setup();
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

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.click(screen.getByRole("button", { name: "Inspect workflow" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect workflow" });

    await user.type(within(inspector).getByLabelText("Workflow name"), "xyz");

    expect(screen.getByTestId("workflow-editor-canvas")).toBe(canvas);
    expect(screen.queryByTestId("loading-state")).not.toBeInTheDocument();
  });

  it("updates graph-backed node edits in the mounted canvas", async () => {
    const user = userEvent.setup();
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

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.click(within(canvas).getByTestId("workflow-graph-node-node-1"));
    const inspector = await screen.findByRole("complementary", { name: "Inspect node" });

    await user.type(within(inspector).getByLabelText("Display name"), "x");

    expect(screen.getByTestId("workflow-editor-canvas")).toBe(canvas);
  });

  it("projects graph-backed node label edits locally without draft-validation RPC calls", async () => {
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponse },
      { method: "workflow.validate", result: invalidValidationResponse },
      {
        method: "workflow.graph.validateDraft",
        handler(_params, callIndex) {
          if (callIndex === 0) {
            return validGraphValidationResponse;
          }
          throw new Error("Draft validation must not run for node label typing.");
        },
      },
    ]);
    render(<App services={services} />);

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    const node = within(canvas).getByTestId("workflow-graph-node-node-1");
    fireEvent.click(node);
    const inspector = await screen.findByRole("complementary", { name: "Inspect node" });

    fireEvent.change(within(inspector).getByLabelText("Display name"), {
      target: { value: "Implement draft" },
    });

    expect(await within(canvas).findByText("Implement draft")).toBeInTheDocument();
    expect(workflowDraftValidationCallCount(services)).toBe(1);
  });

  it("keeps graph-backed node label edits local while draft validation is pending", async () => {
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    let resolvePendingDraftValidation:
      | ((value: typeof validGraphValidationResponse) => void)
      | undefined;
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponse },
      { method: "workflow.validate", result: invalidValidationResponse },
      {
        method: "workflow.graph.validateDraft",
        async handler(_params, callIndex) {
          if (callIndex === 0) {
            return new Promise<typeof validGraphValidationResponse>((resolve) => {
              resolvePendingDraftValidation = resolve;
            });
          }
          throw new Error("Draft validation must not run again while node label typing is dirty.");
        },
      },
    ]);
    render(<App services={services} />);

    const canvas = await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    await waitFor(() => {
      expect(resolvePendingDraftValidation).toBeDefined();
    });
    fireEvent.click(within(canvas).getByTestId("workflow-graph-node-node-1"));
    const inspector = await screen.findByRole("complementary", { name: "Inspect node" });

    fireEvent.change(within(inspector).getByLabelText("Display name"), {
      target: { value: "Implement while pending" },
    });

    expect(await within(canvas).findByText("Implement while pending")).toBeInTheDocument();
    const resolveDraftValidation = resolvePendingDraftValidation;
    if (resolveDraftValidation === undefined) {
      throw new Error("Expected pending draft validation resolver.");
    }
    await act(async () => {
      resolveDraftValidation(validGraphValidationResponse);
    });
    await waitFor(() => {
      expect(screen.getByTestId("workflow-editor-canvas")).toBe(canvas);
      expect(within(canvas).getByText("Implement while pending")).toBeInTheDocument();
    });
    expect(workflowDraftValidationCallCount(services)).toBe(1);
  });

  it("opens workflow inspectors with their own 35 percent screen-width default", async () => {
    const restoreWindowWidth = mockWindowWidth(1600);
    mockSidebarLayout(() => 1600);
    try {
      render(
        <AppProviders services={createTestServices(workflowEditorRoutes())}>
          <SidebarProvider>
            <WorkflowEditorDraftBridgeProvider>
              <div className="relative flex min-h-0" data-testid="app-shell-content">
                <OpenStandardSidebar />
                <WorkflowEditorRoute projectID="" workflowID="workflow-1" />
                <SidebarHost />
              </div>
            </WorkflowEditorDraftBridgeProvider>
          </SidebarProvider>
        </AppProviders>,
      );

      await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
      fireEvent.click(screen.getByRole("button", { name: "Open standard sidebar" }));
      const standardSidebar = await screen.findByRole("complementary", { name: "Settings" });
      fireEvent.keyDown(within(standardSidebar).getByRole("separator", { name: "Resize sidebar" }), {
        key: "Home",
      });
      expect(sidebarWidthStyle(standardSidebar)).toBe("360px");
      fireEvent.click(within(standardSidebar).getByRole("button", { name: "Close" }));
      await waitFor(() => {
        expect(screen.queryByRole("complementary", { name: "Settings" })).not.toBeInTheDocument();
      });

      fireEvent.click(screen.getByRole("button", { name: "Inspect workflow" }));
      const workflowInspector = await screen.findByRole("complementary", { name: "Inspect workflow" });
      expect(sidebarWidthStyle(workflowInspector)).toBe("560px");
      fireEvent.keyDown(within(workflowInspector).getByRole("separator", { name: "Resize sidebar" }), {
        key: "ArrowLeft",
      });
      expect(sidebarWidthStyle(workflowInspector)).toBe("592px");
      fireEvent.click(within(workflowInspector).getByRole("button", { name: "Close" }));
      await waitFor(() => {
        expect(screen.queryByRole("complementary", { name: "Inspect workflow" })).not.toBeInTheDocument();
      });

      fireEvent.click(screen.getByRole("button", { name: "Open standard sidebar" }));
      const reopenedStandardSidebar = await screen.findByRole("complementary", { name: "Settings" });
      expect(sidebarWidthStyle(reopenedStandardSidebar)).toBe("360px");
      fireEvent.click(within(reopenedStandardSidebar).getByRole("button", { name: "Close" }));
      await waitFor(() => {
        expect(screen.queryByRole("complementary", { name: "Settings" })).not.toBeInTheDocument();
      });

      fireEvent.click(screen.getByRole("button", { name: "Inspect workflow" }));
      expect(sidebarWidthStyle(await screen.findByRole("complementary", { name: "Inspect workflow" }))).toBe(
        "592px",
      );
    } finally {
      restoreWindowWidth();
    }
  });

  it("renders read-only edge inspector details from cached workflow data", async () => {
    render(
      <AppProviders services={createTestServices(startupRoutes)}>
        <CachedEdgeInspectorFixture />
      </AppProviders>,
    );

    expect(await screen.findByRole("region", { name: "Route" })).toBeInTheDocument();
    expect(screen.getByText("Label")).toBeInTheDocument();
    expect(screen.getByText("Key")).toBeInTheDocument();
    expect(screen.queryByText("Transition ID")).not.toBeInTheDocument();
    expect(screen.queryByText("Branch key")).not.toBeInTheDocument();
    expect(screen.queryByText("Model-facing description")).not.toBeInTheDocument();
    expect(screen.queryByText("Context mode")).not.toBeInTheDocument();
    expect(screen.queryByText("Context source")).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Derived parameter bindings" })).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Derived provision requirements" })).not.toBeInTheDocument();
  });

  it("renders read-only transition model-facing descriptions when present", async () => {
    const describedDefinition = {
      ...cachedWorkflowDefinition,
      transitionGroups: cachedWorkflowDefinition.transitionGroups.map((group) =>
        group.id === "tg-2" ? { ...group, description: "Finish the workflow when all work is complete." } : group,
      ),
    };
    render(
      <AppProviders services={createTestServices(startupRoutes)}>
        <CachedEdgeInspectorFixture definition={describedDefinition} />
      </AppProviders>,
    );

    expect(await screen.findByRole("region", { name: "Route" })).toBeInTheDocument();
    expect(screen.getByText("Label")).toBeInTheDocument();
    expect(screen.getByText("Key")).toBeInTheDocument();
    expect(screen.getByText("Model-facing description")).toBeInTheDocument();
    expect(screen.getByText("Finish the workflow when all work is complete.")).toBeInTheDocument();
  });

  it("renders read-only node inspector without kind, group, or titled identity behavior islands", async () => {
    render(
      <AppProviders services={createTestServices(startupRoutes)}>
        <CachedNodeInspectorFixture />
      </AppProviders>,
    );

    expect(screen.getAllByRole("heading").length).toBeGreaterThan(0);
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

    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
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
    expect(screen.getByTestId("workflow-editor-route")).toBeInTheDocument();
  });

  it("shows and acknowledges a dirty-draft conflict when the remote workflow version changes", async () => {
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
    expect(await screen.findByRole("complementary", { name: "Unsaved changes" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Simulate remote update" }));
    expect(await screen.findByRole("button", { name: "Keep editing" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "Keep editing" })).not.toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: "Simulate remote update" }));
    expect(await screen.findByRole("button", { name: "Keep editing" })).toBeInTheDocument();
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
          current_version: 1,
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
          current_version: 2,
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
    const unsavedChanges = await screen.findByRole("complementary", { name: "Unsaved changes" });
    const title = within(unsavedChanges).getByRole("heading", { name: "Unsaved changes" });
    const discardButton = within(unsavedChanges).getByRole("button", { name: "Discard" });
    const saveButton = within(unsavedChanges).getByRole("button", { name: "Save" });
    expect(title.compareDocumentPosition(discardButton) & Node.DOCUMENT_POSITION_FOLLOWING).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
    expect(discardButton.compareDocumentPosition(saveButton) & Node.DOCUMENT_POSITION_FOLLOWING).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );

    expect(saveButton).toBeEnabled();
    fireEvent.click(saveButton);

    await waitFor(() => {
      const saveCall = services.transport.calls.find((call) => call.method === "workflow.graph.save");
      expect(saveCall?.params).toMatchObject({
        expected_version: 1,
        metadata: { name: "Locally edited delivery", description: "" },
        workflow_id: "workflow-1",
      });
    });
  });

  it("confirms destructive graph saves from the status island before saving", async () => {
    const destructiveImpact = {
      ...graphSaveImpactResponse,
      removed_node_count: 1,
      removed_edge_count: 2,
      removed_transition_group_count: 1,
      node_task_reference_count: 2,
      edge_task_reference_count: 1,
    };
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponse },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      {
        method: "workflow.graph.validateDraft",
        result: {
          results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
        },
      },
      {
        method: "workflow.graph.savePreview",
        result: {
          current_version: 1,
          validation_results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
          impact: destructiveImpact,
          blockers: [{ code: "confirmation_required", message: "Confirm removal.", count: 3 }],
          can_save: true,
          confirmation_required: true,
        },
      },
      {
        method: "workflow.graph.save",
        result: {
          saved: true,
          definition: workflowDefinitionResponseWithRevision(2).definition,
          current_version: 2,
          validation_results: { draft: { valid: true, errors: [] }, execution: { valid: true, errors: [] } },
          impact: destructiveImpact,
          blockers: [],
          can_save: true,
          confirmation_required: false,
        },
      },
    ]);
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(<App services={services} />);

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    fireEvent.pointerEnter(screen.getByRole("button", { name: "Add node" }));
    fireEvent.click(await screen.findByRole("button", { name: "Agent node" }));
    fireEvent.click(await screen.findByRole("button", { name: "Save" }));

    expect(
      await screen.findByText("This save will permanently remove graph rows. Proceed?"),
    ).toBeInTheDocument();
    expect(screen.getByText("Removed nodes: 1")).toBeInTheDocument();
    expect(screen.getByText("Removed transition groups: 1")).toBeInTheDocument();
    expect(screen.getByText("Removed branches: 2")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Confirm save" }));

    await waitFor(() => {
      const saveCall = services.transport.calls.find((call) => call.method === "workflow.graph.save");
      expect(saveCall?.params).toMatchObject({
        confirmation: {
          expected_removed_node_count: 1,
          expected_removed_transition_group_count: 1,
          expected_removed_edge_count: 2,
          expected_node_task_reference_count: 2,
          expected_edge_task_reference_count: 1,
        },
      });
    });
  });

  it("scrolls the whole unsaved changes island when issue content exceeds the max height", async () => {
    const manyExecutionIssues = workflowValidationResponseWithMessages(
      Array.from({ length: 18 }, (_unused, index) => `Execution issue ${(index + 1).toString()}`),
    );
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponse },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      {
        method: "workflow.graph.validateDraft",
        result: {
          results: {
            draft: { valid: true, errors: [] },
            execution: manyExecutionIssues,
          },
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
    const unsavedChanges = await screen.findByRole("complementary", { name: "Unsaved changes" });

    expect(within(unsavedChanges).getByTestId("floating-notice-header")).toBeInTheDocument();
    expect(within(unsavedChanges).getByRole("button", { name: "Discard" })).toBeInTheDocument();
    expect(within(unsavedChanges).getByRole("button", { name: "Save" })).toBeInTheDocument();
    expect(
      within(unsavedChanges)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(manyExecutionIssues.errors.map((issue) => issue.message));
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

function workflowEditorRoutes() {
  return [
    ...startupRoutes,
    { method: "workflow.get", result: workflowDefinitionResponse },
    { method: "workflow.validate", result: invalidValidationResponse },
    { method: "workflow.graph.validateDraft", result: graphValidationResponse },
  ];
}

function startupRoutesWithSubagentRoles(roleNames: readonly string[]) {
  return [
    {
      method: "server.readiness.get",
      result: {
        auth_ready: true,
        auth_required: true,
        endpoint: "ws://127.0.0.1:53082/rpc",
        protocol_version: protocolVersion,
        ready: true,
        server_build: "1.3.0",
        server_id: "server-1",
        server_version: "1.3.0",
        subagent_roles: roleNames.map((name) => ({ name })),
      },
    },
    ...startupRoutes.filter((route) => route.method !== "server.readiness.get"),
  ];
}

function mockWindowWidth(width: number): () => void {
  const descriptor = Object.getOwnPropertyDescriptor(window, "innerWidth");
  Object.defineProperty(window, "innerWidth", { configurable: true, value: width });
  return () => {
    if (descriptor === undefined) {
      Reflect.deleteProperty(window, "innerWidth");
      return;
    }
    Object.defineProperty(window, "innerWidth", descriptor);
  };
}

function parameterKeys(container: HTMLElement): readonly string[] {
  return within(container)
    .getAllByTestId("workflow-parameter")
    .map((field) => field.dataset.parameterKey ?? "");
}

function workflowDraftValidationCallCount(services: ReturnType<typeof createTestServices>): number {
  return services.transport.calls.filter((call) => call.method === "workflow.graph.validateDraft").length;
}

function workflowGraphSaveEdges(params: unknown): readonly Readonly<Record<string, unknown>>[] {
  if (!isRecord(params) || !isRecord(params.graph) || !Array.isArray(params.graph.edges)) {
    throw new Error("Expected workflow graph save params with graph edges.");
  }
  if (!params.graph.edges.every(isRecord)) {
    throw new Error("Expected workflow graph save edges to be objects.");
  }
  return params.graph.edges;
}

function hasRepairedGroupedBranchFanout(params: unknown): boolean {
  if (!isRecord(params) || !isRecord(params.graph) || !Array.isArray(params.graph.edges)) {
    return false;
  }
  const branchTransitionGroupIDs = params.graph.edges
    .filter(isRecord)
    .filter((edge) => edge.target_node_id === "impl-a" || edge.target_node_id === "impl-b")
    .map((edge) => edge.transition_group_id);
  return (
    branchTransitionGroupIDs.length === 2 &&
    branchTransitionGroupIDs.every((transitionGroupID) => transitionGroupID === "tg-plan-new-agent")
  );
}

function isRecord(value: unknown): value is Readonly<Record<string, unknown>> {
  return typeof value === "object" && value !== null;
}

function dispatchMouseEvent(
  target: Document | Element | Window,
  view: Window & typeof globalThis,
  type: "mousedown" | "mousemove" | "mouseup",
  options: MouseEventInit,
): void {
  const event = new view.MouseEvent(type, { bubbles: true, cancelable: true, ...options });
  Object.defineProperty(event, "view", { value: view });
  fireEvent(target, event);
}

function dragWorkflowNodeOut(card: HTMLElement): void {
  const eventView = card.ownerDocument.defaultView;
  if (eventView === null) {
    throw new Error("Expected test document to have a default window");
  }
  const restoreElementFromPoint = mockDocumentElementFromPoint(
    vi.fn<typeof document.elementFromPoint>(() => card),
  );
  try {
    dispatchMouseEvent(card, eventView, "mousedown", { button: 0, clientX: 12, clientY: 18 });
    dispatchMouseEvent(document, eventView, "mousemove", { buttons: 1, clientX: 28, clientY: 34 });
    dispatchMouseEvent(document, eventView, "mousemove", { buttons: 1, clientX: 500, clientY: 500 });
    dispatchMouseEvent(document, eventView, "mouseup", { clientX: 500, clientY: 500 });
  } finally {
    restoreElementFromPoint();
  }
}

function mockDocumentElementFromPoint(elementFromPoint: typeof document.elementFromPoint): () => void {
  const originalElementFromPoint = Object.getOwnPropertyDescriptor(document, "elementFromPoint");
  const fallbackElementFromPoint: typeof document.elementFromPoint = () => null;
  Object.defineProperty(document, "elementFromPoint", {
    configurable: true,
    value: elementFromPoint,
  });
  return () => {
    if (originalElementFromPoint === undefined) {
      Object.defineProperty(document, "elementFromPoint", {
        configurable: true,
        value: fallbackElementFromPoint,
      });
      return;
    }
    Object.defineProperty(document, "elementFromPoint", originalElementFromPoint);
  };
}

function expectIdentifierInputCorrectionsDisabled(input: HTMLElement): void {
  expect(input).toHaveAttribute("autocapitalize", "none");
  expect(input).toHaveAttribute("autocomplete", "off");
  expect(input).toHaveAttribute("autocorrect", "off");
  expect(input).toHaveAttribute("spellcheck", "false");
}

function mockSidebarLayout(shellWidth: () => number): void {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function getBoundingClientRect(
    this: HTMLElement,
  ) {
    if (this.dataset.testid === "app-shell-content") {
      return domRect({ height: 720, width: shellWidth() });
    }
    return domRect({ height: 720, width: 560 });
  });
}

function mockParameterLayout(): void {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function getBoundingClientRect(
    this: HTMLElement,
  ) {
    if (this.dataset.testid === "workflow-parameter") {
      return domRect({ height: 96, top: this.dataset.parameterKey === "summary" ? 140 : 20, width: 320 });
    }
    return domRect({ height: 96, top: 20, width: 320 });
  });
}

function domRect({
  height,
  top = 0,
  width,
}: Readonly<{ height: number; top?: number; width: number }>): DOMRect {
  return {
    bottom: top + height,
    height,
    left: 0,
    right: width,
    top,
    width,
    x: 0,
    y: top,
    toJSON: () => ({}),
  };
}

function CachedEdgeInspectorFixture({
  definition = cachedWorkflowDefinition,
  edgeID = "edge-2",
}: Readonly<{
  definition?: typeof cachedWorkflowDefinition | undefined;
  edgeID?: string | undefined;
}>) {
  const queryClient = useQueryClient();
  useEffect(() => {
    queryClient.setQueryData(queryKeys.workflowDefinition("workflow-1"), definition);
    queryClient.setQueryData(queryKeys.workflowValidation("workflow-1", "execution"), cachedValidation);
  }, [definition, queryClient]);
  return <WorkflowInspectorSidebar selection={{ kind: "edge", edgeID }} workflowID="workflow-1" />;
}

function CachedNodeInspectorFixture() {
  const queryClient = useQueryClient();
  useEffect(() => {
    queryClient.setQueryData(queryKeys.workflowDefinition("workflow-1"), cachedWorkflowDefinition);
    queryClient.setQueryData(queryKeys.workflowValidation("workflow-1", "execution"), cachedValidation);
  }, [queryClient]);
  return <WorkflowInspectorSidebar selection={{ kind: "node", nodeID: "node-1" }} workflowID="workflow-1" />;
}

function workflowDefinitionResponseWithAgentRole(subagentRole: string) {
  return {
    definition: {
      ...workflowDefinitionResponse.definition,
      nodes: workflowDefinitionResponse.definition.nodes.map((node) =>
        node.id === "node-1" ? { ...node, subagent_role: subagentRole } : node,
      ),
    },
  };
}

function OpenStandardSidebar() {
  const { openSidebar } = useSidebar();
  return (
    <button
      onClick={() => {
        void openSidebar({
          content: <p>Default sidebar content</p>,
          kind: "custom",
          title: "Settings",
        });
      }}
      type="button"
    >
      Open standard sidebar
    </button>
  );
}

function OpenEdgeInspectorButton({ edgeID = "edge-2" }: Readonly<{ edgeID?: string | undefined }>) {
  const { openSidebar } = useSidebar();
  return (
    <button
      onClick={() => {
        void openSidebar({
          kind: "workflowInspect",
          mode: "overlay",
          selection: { kind: "edge", edgeID },
          workflowID: "workflow-1",
        });
      }}
      type="button"
    >
      Open edge inspector
    </button>
  );
}

function sidebarWidthStyle(sidebar: HTMLElement): string {
  return sidebar.style.getPropertyValue("--app-sidebar-width");
}

function nativeBridgeWithClipboard(copied: string[]): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    capabilities: {
      ...base.capabilities,
      clipboard: { ...base.capabilities.clipboard, writeText: true },
    },
    clipboard: {
      ...base.clipboard,
      async writeText(value): Promise<void> {
        copied.push(value);
      },
    },
  };
}

function nativeWorkflowDeleteDialogBridge(opened: NativeDialogWindowOptions[]): NativeBridge {
  const base = createBrowserNativeBridge();
  const handlers = new Set<(confirmation: NativeWorkflowGraphDeleteConfirmation) => void>();
  return {
    ...base,
    capabilities: {
      ...base.capabilities,
      dialogWindows: true,
    },
    dialogs: {
      ...base.dialogs,
      async openWindow(options): Promise<void> {
        opened.push(options);
      },
    },
    workflowEditor: {
      async confirmGraphDelete(confirmation): Promise<void> {
        for (const handler of handlers) {
          handler(confirmation);
        }
      },
      async onGraphDeleteConfirmed(handler): Promise<() => void> {
        handlers.add(handler);
        return () => {
          handlers.delete(handler);
        };
      },
    },
  };
}

function nativeWorkflowEntityDeleteDialogBridge(opened: NativeDialogWindowOptions[]): NativeBridge {
  const base = createBrowserNativeBridge();
  const handlers = new Set<(event: NativeWorkflowDeleted) => void>();
  return {
    ...base,
    capabilities: {
      ...base.capabilities,
      dialogWindows: true,
    },
    dialogs: {
      ...base.dialogs,
      async openWindow(options): Promise<void> {
        opened.push(options);
      },
    },
    workflowDeletion: {
      async notifyDeleted(event): Promise<void> {
        for (const handler of handlers) {
          handler(event);
        }
      },
      async onDeleted(handler): Promise<() => void> {
        handlers.add(handler);
        return () => {
          handlers.delete(handler);
        };
      },
    },
  };
}

function nativeWorkflowDeleteWindowBridge(
  onClose: () => void,
  onDeleted: (event: NativeWorkflowDeleted) => void,
): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    window: {
      ...base.window,
      async closeCurrent(): Promise<void> {
        onClose();
      },
    },
    workflowDeletion: {
      ...base.workflowDeletion,
      async notifyDeleted(event): Promise<void> {
        onDeleted(event);
      },
    },
  };
}

function nativeWorkflowDeleteListenerFailureBridge(): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    workflowEditor: {
      ...base.workflowEditor,
      async onGraphDeleteConfirmed(): Promise<() => void> {
        throw new Error("listener unavailable");
      },
    },
  };
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

function EdgeContextMenuDeleteDriver() {
  const controller = useWorkflowEditorDraftController("workflow-1");
  if (controller === null) {
    return null;
  }
  return (
    <div>
      <span data-testid="edge-delete-driver-edges">
        {controller.draft.edges.map((edge) => edge.id).join(",")}
      </span>
      <svg>
        <WorkflowGraphEdge
          data={{
            contextMode: "compact_and_continue_session",
            entityID: "edge-2",
            entityKind: "edge",
            hasError: false,
            label: "",
            routePoints: [
              { x: 0, y: 0 },
              { x: 100, y: 0 },
            ],
            transitionGroupID: "tg-2",
          }}
          id="edge-delete-driver"
          onDeleteSelection={(selection) => {
            if (selection.kind === "edge") {
              controller.dispatch({ edgeID: selection.edgeID, type: "deleteEdge" });
            }
          }}
          onInspect={() => undefined}
          onSelectContextMenu={() => undefined}
          source="join"
          sourceX={0}
          sourceY={0}
          target="done"
          targetX={100}
          targetY={0}
          type="workflow"
        />
      </svg>
    </div>
  );
}

function WorkflowNodeGroupProbe({ nodeID, testID }: Readonly<{ nodeID: string; testID: string }>) {
  const controller = useWorkflowEditorDraftController("workflow-1");
  if (controller === null) {
    return null;
  }
  const node = controller.draft.nodes.find((item) => item.id === nodeID);
  const groupID = node?.groupID;
  return <span data-testid={testID}>{groupID === undefined || groupID.length === 0 ? "none" : groupID}</span>;
}

function WorkflowFanoutProbe() {
  const controller = useWorkflowEditorDraftController("workflow-1");
  if (controller === null) {
    return null;
  }
  const branchEdges = controller.draft.edges
    .filter((edge) => edge.targetNodeID === "impl-a" || edge.targetNodeID === "impl-b")
    .sort((left, right) => left.targetNodeID.localeCompare(right.targetNodeID));
  return (
    <div>
      <span data-testid="workflow-fanout-probe">
        {branchEdges.map((edge) => edge.transitionGroupID).join(",")}
      </span>
      <button
        onClick={() => {
          controller.dispatch({ edgeID: "edge-plan-implement", type: "deleteEdge" });
        }}
        type="button"
      >
        Delete stale branch edge
      </button>
      <button
        onClick={() => {
          controller.dispatch({
            input: {
              edgeID: "edge-plan-implement-repair",
              sourceNodeID: "plan",
              targetNodeID: "impl-b",
              transitionGroupID: "tg-unused-repair",
            },
            type: "connectNodes",
          });
        }}
        type="button"
      >
        Drag Plan to Implement
      </button>
    </div>
  );
}

function StaleNativeGraphDeleteDriver() {
  const controller = useWorkflowEditorDraftController("workflow-1");
  if (controller === null) {
    return null;
  }
  return (
    <div>
      <span data-testid="stale-native-delete-driver-edges">
        {controller.draft.edges.map((edge) => edge.id).join(",")}
      </span>
      <button
        onClick={() => {
          controller.dispatch({
            input: {
              edgeID: "edge-stale",
              sourceNodeID: "node-1",
              targetNodeID: "done",
              transitionGroupID: "tg-stale",
            },
            type: "connectNodes",
          });
        }}
        type="button"
      >
        Add stale edge
      </button>
    </div>
  );
}

function StalePromptNativeGraphDeleteDriver() {
  const controller = useWorkflowEditorDraftController("workflow-1");
  if (controller === null) {
    return null;
  }
  const promptTemplate = controller.draft.edges.find((edge) => edge.id === "edge-1")?.promptTemplate ?? "";
  return (
    <div>
      <span data-testid="stale-native-delete-driver-prompt">{promptTemplate}</span>
      <button
        onClick={() => {
          controller.dispatch({
            edgeID: "edge-1",
            promptTemplate: "Added stale prompt.",
            type: "editEdgePrompt",
          });
        }}
        type="button"
      >
        Add stale prompt
      </button>
    </div>
  );
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
  version: 1,
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
      version: 1,
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
        input_fields: [{ name: "summary", description: "Summary" }],
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
        parameters: [{ key: "summary", description: "Summary" }],
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

const workflowDefinitionResponseWithReviewBranch = {
  definition: {
    ...workflowDefinitionResponse.definition,
    nodes: [
      ...workflowDefinitionResponse.definition.nodes,
      {
        id: "review",
        workflow_id: "workflow-1",
        key: "review",
        kind: "agent",
        display_name: "Review",
        subagent_role: "reviewer",
        prompt_template: "Legacy review prompt.",
      },
    ],
    transition_groups: [
      ...workflowDefinitionResponse.definition.transition_groups,
      {
        id: "tg-review",
        workflow_id: "workflow-1",
        source_node_id: "node-1",
        transition_id: "review",
        display_name: "Review",
      },
    ],
    edges: [
      ...workflowDefinitionResponse.definition.edges,
      {
        id: "edge-review",
        workflow_id: "workflow-1",
        transition_group_id: "tg-review",
        key: "review",
        target_node_id: "review",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
        prompt_template: "Review the task.",
        parameters: [{ key: "summary", description: "Summary" }],
      },
    ],
  },
};

const workflowDefinitionResponseWithStartAndReviewBranch = {
  definition: {
    ...workflowDefinitionResponseWithReviewBranch.definition,
    nodes: [
      {
        id: "start",
        workflow_id: "workflow-1",
        key: "start",
        kind: "start",
        display_name: "Start",
      },
      ...workflowDefinitionResponseWithReviewBranch.definition.nodes,
    ],
    transition_groups: [
      {
        id: "tg-start",
        workflow_id: "workflow-1",
        source_node_id: "start",
        transition_id: "implement",
        display_name: "Implement",
      },
      ...workflowDefinitionResponseWithReviewBranch.definition.transition_groups,
    ],
    edges: [
      {
        id: "edge-start",
        workflow_id: "workflow-1",
        transition_group_id: "tg-start",
        key: "implement",
        target_node_id: "node-1",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
        prompt_template: "Implement the task.",
      },
      ...workflowDefinitionResponseWithReviewBranch.definition.edges,
    ],
  },
};

const workflowDefinitionResponseWithSeparateGroupedBranchTransitions = {
  definition: {
    workflow: {
      id: "workflow-1",
      name: "Delivery",
      description: "",
      version: 1,
    },
    node_groups: [
      {
        group_id: "group-parallel",
        workflow_id: "workflow-1",
        group_key: "parallel",
        display_name: "Implement parallel",
        sort_order: 1,
        node_ids: ["impl-a", "impl-b", "join"],
      },
    ],
    nodes: [
      {
        id: "start",
        workflow_id: "workflow-1",
        key: "backlog",
        kind: "start",
        display_name: "Backlog",
      },
      {
        id: "plan",
        workflow_id: "workflow-1",
        key: "plan",
        kind: "agent",
        display_name: "Plan",
        subagent_role: "fast",
        prompt_template: "Plan.",
      },
      {
        id: "impl-a",
        workflow_id: "workflow-1",
        key: "new_agent",
        kind: "agent",
        display_name: "New agent",
        group_id: "group-parallel",
        group_key: "parallel",
        subagent_role: "default",
        prompt_template: "A.",
      },
      {
        id: "impl-b",
        workflow_id: "workflow-1",
        key: "implement",
        kind: "agent",
        display_name: "Implement",
        group_id: "group-parallel",
        group_key: "parallel",
        subagent_role: "fast",
        prompt_template: "B.",
      },
      {
        id: "join",
        workflow_id: "workflow-1",
        key: "implement_join",
        kind: "join",
        display_name: "Implement join",
        group_id: "group-parallel",
        group_key: "parallel",
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
        id: "tg-start-plan",
        workflow_id: "workflow-1",
        source_node_id: "start",
        transition_id: "start",
        display_name: "Start",
      },
      {
        id: "tg-plan-new-agent",
        workflow_id: "workflow-1",
        source_node_id: "plan",
        transition_id: "new_agent",
        display_name: "Implement",
      },
      {
        id: "tg-plan-implement",
        workflow_id: "workflow-1",
        source_node_id: "plan",
        transition_id: "implement",
        display_name: "Implement",
      },
      {
        id: "tg-impl-a-join",
        workflow_id: "workflow-1",
        source_node_id: "impl-a",
        transition_id: "join",
        display_name: "Implement join",
      },
      {
        id: "tg-impl-b-join",
        workflow_id: "workflow-1",
        source_node_id: "impl-b",
        transition_id: "join",
        display_name: "Implement join",
      },
      {
        id: "tg-join-done",
        workflow_id: "workflow-1",
        source_node_id: "join",
        transition_id: "done",
        display_name: "Done",
      },
    ],
    edges: [
      {
        id: "edge-start-plan",
        workflow_id: "workflow-1",
        transition_group_id: "tg-start-plan",
        key: "start",
        target_node_id: "plan",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
        prompt_template: "Plan.",
      },
      {
        id: "edge-plan-new-agent",
        workflow_id: "workflow-1",
        transition_group_id: "tg-plan-new-agent",
        key: "new_agent",
        target_node_id: "impl-a",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
        prompt_template: "A.",
      },
      {
        id: "edge-plan-implement",
        workflow_id: "workflow-1",
        transition_group_id: "tg-plan-implement",
        key: "implement",
        target_node_id: "impl-b",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
        prompt_template: "B.",
      },
      {
        id: "edge-impl-a-join",
        workflow_id: "workflow-1",
        transition_group_id: "tg-impl-a-join",
        key: "join_a",
        target_node_id: "join",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
      },
      {
        id: "edge-impl-b-join",
        workflow_id: "workflow-1",
        transition_group_id: "tg-impl-b-join",
        key: "join_b",
        target_node_id: "join",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
      },
      {
        id: "edge-join-done",
        workflow_id: "workflow-1",
        transition_group_id: "tg-join-done",
        key: "done",
        target_node_id: "done",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
      },
    ],
  },
};

const workflowDefinitionResponseWithValidGroupedBranches = {
  definition: {
    ...workflowDefinitionResponseWithSeparateGroupedBranchTransitions.definition,
    transition_groups: workflowDefinitionResponseWithSeparateGroupedBranchTransitions.definition.transition_groups.filter(
      (group) => group.id !== "tg-plan-implement",
    ),
    edges: workflowDefinitionResponseWithSeparateGroupedBranchTransitions.definition.edges.map((edge) =>
      edge.id === "edge-plan-implement" ? { ...edge, transition_group_id: "tg-plan-new-agent" } : edge,
    ),
  },
};

const workflowDeleteImpactResponse = {
  workflow_id: "workflow-1",
  version: 1,
  project_count: 1,
  link_count: 1,
  default_replacement_project_count: 0,
  task_count: 2,
  active_run_count: 0,
  runnable_run_count: 0,
  blocked_task_count: 0,
};

const workflowDeletePreviewResponse = {
  impact: workflowDeleteImpactResponse,
};

const workflowDeleteResponse = {
  deleted: true,
  impact: workflowDeleteImpactResponse,
  blockers: [],
};

const workflowDefinitionResponseWithStartNode = {
  definition: {
    ...workflowDefinitionResponse.definition,
    nodes: [
      {
        id: "start",
        workflow_id: "workflow-1",
        key: "start",
        kind: "start",
        display_name: "Start",
      },
      ...workflowDefinitionResponse.definition.nodes,
    ],
    transition_groups: [
      {
        id: "tg-start",
        workflow_id: "workflow-1",
        source_node_id: "start",
        transition_id: "implement",
        display_name: "Implement",
      },
      ...workflowDefinitionResponse.definition.transition_groups,
    ],
    edges: [
      {
        id: "edge-start",
        workflow_id: "workflow-1",
        transition_group_id: "tg-start",
        key: "implement",
        target_node_id: "node-1",
        requires_approval: false,
        context_mode: "new_session",
        context_source: { kind: "immediate_source" },
        prompt_template: "Implement the task.",
      },
      ...workflowDefinitionResponse.definition.edges,
    ],
  },
};

const workflowDefinitionResponseWithUnrelatedAgent = {
  definition: {
    ...workflowDefinitionResponseWithStartNode.definition,
    nodes: [
      ...workflowDefinitionResponseWithStartNode.definition.nodes,
      {
        id: "review",
        workflow_id: "workflow-1",
        key: "review",
        kind: "agent",
        display_name: "Review",
        subagent_role: "coder",
        prompt_template: "Review the task.",
      },
    ],
  },
};

const workflowDefinitionResponseWithLoopbackTarget = {
  definition: {
    ...workflowDefinitionResponseWithStartNode.definition,
    transition_groups: workflowDefinitionResponseWithStartNode.definition.transition_groups.map((group) =>
      group.id === "tg-2" ? { ...group, transition_id: "rework", display_name: "Rework" } : group,
    ),
    edges: workflowDefinitionResponseWithStartNode.definition.edges.map((edge) =>
      edge.id === "edge-2"
        ? {
            ...edge,
            id: "edge-rework",
            key: "rework",
            target_node_id: "node-1",
            requires_approval: false,
            context_mode: "new_session",
            context_source: { kind: "immediate_source" },
            prompt_template: "Rework the task.",
          }
        : edge,
    ),
  },
};

const workflowDefinitionResponseWithStartGroupAndUnrelatedAgent = {
  definition: {
    ...workflowDefinitionResponseWithUnrelatedAgent.definition,
    node_groups: [
      {
        ...workflowDefinitionResponseWithUnrelatedAgent.definition.node_groups[0],
        node_ids: ["node-1", "join"],
      },
    ],
    nodes: workflowDefinitionResponseWithUnrelatedAgent.definition.nodes.map((node) =>
      node.id === "join" ? { ...node, group_id: "group-1", group_key: "core" } : node,
    ),
  },
};

function workflowDefinitionResponseWithRevision(version: number) {
  return {
    definition: {
      ...workflowDefinitionResponse.definition,
      workflow: {
        ...workflowDefinitionResponse.definition.workflow,
        version: version,
      },
    },
  };
}

function workflowDefinitionResponseWithEdgeApproval(
  edgeID: string,
  requiresApproval: boolean,
  version: number,
) {
  return {
    definition: {
      ...workflowDefinitionResponse.definition,
      workflow: {
        ...workflowDefinitionResponse.definition.workflow,
        version: version,
      },
      edges: workflowDefinitionResponse.definition.edges.map((edge) =>
        edge.id === edgeID ? { ...edge, requires_approval: requiresApproval } : edge,
      ),
    },
  };
}

function workflowDefinitionResponseWithEdgePrompt(edgeID: string, promptTemplate: string, version: number) {
  return {
    definition: {
      ...workflowDefinitionResponseWithReviewBranch.definition,
      workflow: {
        ...workflowDefinitionResponseWithReviewBranch.definition.workflow,
        version: version,
      },
      edges: workflowDefinitionResponseWithReviewBranch.definition.edges.map((edge) =>
        edge.id === edgeID ? { ...edge, prompt_template: promptTemplate } : edge,
      ),
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
      details: {
        input_name: "summary",
        provider_edge_id: "edge-provider",
      },
      related_ids: [],
      blocks_context: true,
    },
  ],
};

function workflowValidationResponseWithMessages(messages: readonly string[]) {
  return {
    valid: false,
    errors: messages.map((message, index) => {
      const idSuffix = (index + 1).toString();
      return {
        code: "workflow.validation.invalid",
        message,
        workflow_id: "workflow-1",
        node_id: "node-1",
        transition_group_id: `tg-${idSuffix}`,
        edge_id: `edge-${idSuffix}`,
        related_ids: [],
        blocks_context: true,
      };
    }),
  };
}

const graphValidationResponse = {
  derived_wiring: {
    nodes: [
      {
        node_id: "join",
        join_output_fields: [{ name: "summary", description: "Summary" }],
      },
    ],
    edges: [
      {
        edge_id: "edge-1",
        required_provider_fields: [{ name: "summary", description: "Summary" }],
      },
    ],
  },
  results: {
    draft: invalidValidationResponse,
    execution: invalidValidationResponse,
  },
};

const validGraphValidationResponse = {
  derived_wiring: {
    edges: [],
    nodes: [],
    transition_groups: [],
  },
  results: {
    draft: { errors: [], valid: true },
    execution: { errors: [], valid: true },
  },
};

const emptyAgentPromptExecutionValidationResponse = {
  valid: false,
  errors: [
    {
      code: "workflow.validation.transition_prompt_required",
      message: "transition into an agent node requires a prompt",
      workflow_id: "workflow-1",
      node_id: "review",
      transition_group_id: "tg-review",
      edge_id: "edge-review",
      related_ids: [],
      blocks_context: true,
    },
  ],
};

const emptyAgentPromptExecutionGraphValidationResponse = {
  derived_wiring: graphValidationResponse.derived_wiring,
  results: {
    draft: { errors: [], valid: true },
    execution: emptyAgentPromptExecutionValidationResponse,
  },
};

const invalidNodeGroupValidationResponse = {
  derived_wiring: {
    edges: [],
    nodes: [],
    transition_groups: [],
  },
  results: {
    draft: {
      errors: [
        {
          blocks_context: true,
          code: "workflow.validation.invalid_node_group",
          message:
            "Node Backlog cannot directly fan out into a node group yet; insert one split agent after it, fan out from that agent into the group, then join the branches",
          related_ids: ["group-1"],
          workflow_id: "workflow-1",
        },
      ],
      valid: false,
    },
    execution: { errors: [], valid: true },
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
  workflow: { id: "workflow-1", name: "Delivery", description: "", version: 1 },
  derivedWiring: { diagnostics: [], edges: [], nodes: [], transitionGroups: [] },
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
      inputFields: [{ name: "summary", description: "Summary" }],
      joinInputProviders: [],
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
      inputFields: [],
      joinInputProviders: [],
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
      inputFields: [],
      joinInputProviders: [],
      outputFields: [],
    },
  ],
  transitionGroups: [
    { description: "", id: "tg-1", workflowID: "workflow-1", sourceNodeID: "node-1", transitionID: "join", name: "Join" },
    { description: "", id: "tg-2", workflowID: "workflow-1", sourceNodeID: "join", transitionID: "done", name: "Done" },
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
      details: { fieldName: "", inputName: "summary", placeholder: "", providerEdgeID: "edge-provider" },
      relatedIDs: [],
      blocksContext: true,
    },
  ],
};
