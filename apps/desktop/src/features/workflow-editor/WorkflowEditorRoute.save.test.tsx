import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, vi } from "vitest";

import { App } from "../../App";
import { AppProviders } from "../../app/AppProviders";
import { SidebarHost } from "../../app/sidebar";
import { SidebarProvider } from "../../app/sidebarProvider";
import { WorkflowEditorRoute } from "./WorkflowEditorRoute";
import { WorkflowEditorDraftBridgeProvider } from "./workflowEditorDraftBridge";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";
import {
  OpenEdgeInspectorButton,
  WorkflowConflictDriver,
  WorkflowMetadataEditDriver,
} from "./workflowEditorRouteTestDrivers";
import {
  MockResizeObserver,
  workflowDraftValidationCallCount,
  workflowGraphSaveEdges,
} from "./workflowEditorRouteTestUtils";
import {
  blockedDraftGraphValidationResponse,
  blockedDraftValidationMessage,
  emptyAgentPromptExecutionGraphValidationResponse,
  graphSaveImpactResponse,
  graphValidationResponse,
  invalidValidationResponse,
  validGraphValidationResponse,
  workflowValidationResponseWithMessages,
} from "./workflowEditorRouteValidationFixtures";
import {
  workflowDefinitionResponse,
  workflowDefinitionResponseWithEdgePrompt,
  workflowDefinitionResponseWithReviewBranch,
  workflowDefinitionResponseWithRevision,
} from "./workflowEditorRouteWorkflowFixtures";

describe("WorkflowEditorRoute save and draft lifecycle", () => {
  const originalResizeObserver = globalThis.ResizeObserver;

  beforeEach(() => {
    globalThis.ResizeObserver = MockResizeObserver;
  });

  afterEach(() => {
    globalThis.ResizeObserver = originalResizeObserver;
    vi.restoreAllMocks();
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
    const inspector = await screen.findByRole("complementary", { name: "Inspect transition" });
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
    const inspector = await screen.findByRole("complementary", { name: "Inspect transition" });
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


  it("surfaces structured draft validation issues when a dirty-graph save is blocked", async () => {
    const user = userEvent.setup();
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponseWithReviewBranch },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
      {
        method: "workflow.graph.validateDraft",
        handler(_params, callIndex) {
          return callIndex === 0 ? validGraphValidationResponse : blockedDraftGraphValidationResponse;
        },
      },
    ]);
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
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

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });
    await waitFor(() => {
      expect(workflowDraftValidationCallCount(services)).toBe(1);
    });
    fireEvent.click(screen.getByRole("button", { name: "Open edge inspector" }));
    const inspector = await screen.findByRole("complementary", { name: "Inspect transition" });
    await user.clear(within(inspector).getByRole("textbox", { name: "Prompt" }));

    const unsavedChanges = await screen.findByRole("complementary", { name: "Unsaved changes" });
    fireEvent.click(within(unsavedChanges).getByRole("button", { name: "Save" }));

    // The blocked save must surface the structured per-issue detail the
    // validation returned, not just the generic blocker summary, even though
    // the dirty graph disables the background validation query.
    await within(unsavedChanges).findByText(blockedDraftValidationMessage);
    expect(services.transport.calls.map((call) => call.method)).not.toContain("workflow.graph.savePreview");

    // Editing the graph again drops the stale issue: it may reference rows the
    // next edit removes, so the next save attempt re-validates from scratch.
    await user.type(within(inspector).getByRole("textbox", { name: "Prompt" }), "Review the change.");
    await waitFor(() => {
      expect(within(unsavedChanges).queryByText(blockedDraftValidationMessage)).toBeNull();
    });
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
