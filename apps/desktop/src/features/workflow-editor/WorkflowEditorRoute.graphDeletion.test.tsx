import {
  type NativeDialogWindowOptions,
} from "@app/native-bridge";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, vi } from "vitest";

import { App } from "../../App";
import { AppProviders } from "../../app/AppProviders";
import { SidebarProvider } from "../../app/sidebarProvider";
import { WorkflowEditorRoute } from "./WorkflowEditorRoute";
import { WorkflowEditorDraftBridgeProvider } from "./workflowEditorDraftBridge";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";
import {
  EdgeContextMenuDeleteDriver,
  StaleNativeGraphDeleteDriver,
  StalePromptNativeGraphDeleteDriver,
  WorkflowFanoutProbe,
  WorkflowNodeGroupProbe,
} from "./workflowEditorRouteTestDrivers";
import {
  MockResizeObserver,
  dispatchMouseEvent,
  dragWorkflowNodeOut,
  hasRepairedGroupedBranchFanout,
  mockDocumentElementFromPoint,
  nativeWorkflowDeleteDialogBridge,
  nativeWorkflowDeleteListenerFailureBridge,
  workflowDraftValidationCallCount,
} from "./workflowEditorRouteTestUtils";
import {
  graphSaveImpactResponse,
  graphValidationResponse,
  invalidNodeGroupValidationResponse,
  invalidValidationResponse,
  validGraphValidationResponse,
} from "./workflowEditorRouteValidationFixtures";
import {
  workflowDefinitionResponse,
  workflowDefinitionResponseWithReviewBranch,
  workflowDefinitionResponseWithSeparateGroupedBranchTransitions,
  workflowDefinitionResponseWithStartGroupAndUnrelatedAgent,
  workflowDefinitionResponseWithStartNode,
  workflowDefinitionResponseWithValidGroupedBranches,
} from "./workflowEditorRouteWorkflowFixtures";

describe("WorkflowEditorRoute graph deletion and extraction", () => {
  const originalResizeObserver = globalThis.ResizeObserver;

  beforeEach(() => {
    globalThis.ResizeObserver = MockResizeObserver;
  });

  afterEach(() => {
    globalThis.ResizeObserver = originalResizeObserver;
    vi.restoreAllMocks();
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
});
