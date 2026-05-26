/* eslint-disable max-lines -- Workflow editor route test fixtures are intentionally colocated with route scenarios. */
import { createBrowserNativeBridge, type NativeBridge } from "@builder/desktop-native-bridge";
import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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
    expect(within(issues).getAllByRole("list").length).toBeGreaterThan(0);
    expect(screen.getByTestId("route-transition-frame")).not.toHaveClass("p-[var(--space-2)]");
    expect(screen.getByTestId("workflow-editor-route")).toHaveClass(
      "fixed",
      "inset-0",
      "h-screen",
      "w-screen",
    );
    expect(screen.getByTestId("workflow-editor-route")).not.toHaveClass("p-[var(--space-2)]");
    const legend = screen.getByRole("complementary", { name: "Legend" });
    expect(legend).toHaveClass(
      "floating-notice-expanded",
      "left-[var(--space-2)]",
      "bottom-[var(--space-2)]",
      "h-[204px]",
      "p-[var(--space-2)]",
    );
    const edgeSwatches = within(legend).getAllByTestId("workflow-legend-edge-swatch");
    expect(edgeSwatches[0]).toHaveAttribute("width", "22");
    expect(edgeSwatches[0]).toHaveAttribute("height", "6");
    expect(edgeSwatches[0]).toHaveAttribute("viewBox", "0 0 22 6");
    expect(within(legend).getAllByTestId("workflow-legend-edge-line")[0]).toHaveAttribute(
      "stroke-width",
      "1.25",
    );
    const nodeSwatches = within(legend).getAllByTestId("workflow-legend-node-swatch");
    expect(nodeSwatches[0]).toHaveClass("h-[9px]", "w-[14px]", "rounded-[2px]", "border");
    expect(nodeSwatches.at(-1)).toHaveClass("h-[10px]", "w-[10px]", "rounded-[2px]", "rotate-45");
    expect(nodeSwatches.at(-1)).not.toHaveClass("h-[9px]", "w-[14px]");
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
    expect(within(nodeInspector).getByRole("heading", { name: "Inspect node" })).toHaveClass(
      "whitespace-nowrap",
    );
    const nodeIDButton = within(nodeInspector).getByRole("button", { name: "Copy node ID node-1" });
    expect(nodeIDButton).toHaveClass("text-xs", "grid", "min-w-0", "justify-items-end");
    fireEvent.click(nodeIDButton);
    await waitFor(() => {
      expect(copied).toEqual(["node-1"]);
    });
    const assignee = within(nodeInspector).getByRole("combobox", { name: "Assignee" });
    fireEvent.click(assignee);
    fireEvent.click(await within(nodeInspector).findByRole("option", { name: "reviewer" }));

    fireEvent.click(screen.getByTestId("workflow-graph-group-group-1"));
    expect(await screen.findByRole("complementary", { name: "Inspect group" })).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("workflow-join-diamond"));
    expect(await screen.findByRole("complementary", { name: "Inspect node" })).toBeInTheDocument();
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
    const assignee = within(nodeInspector).getByRole("combobox", { name: "Assignee" });

    fireEvent.click(assignee);
    expect(await within(nodeInspector).findByRole("option", { name: "legacy_coder" })).toBeInTheDocument();
  });

  it("edits required inputs as draggable sidebar islands with top insertion", async () => {
    const user = userEvent.setup();
    mockInputFieldLayout();
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
    const nodeInspector = await screen.findByRole("complementary", { name: "Inspect node" });
    const addButton = within(nodeInspector).getByRole("button", { name: "Add required input" });
    const initialFieldTitle = within(nodeInspector).getByRole("button", { name: "summary" });

    expect(addButton.compareDocumentPosition(initialFieldTitle) & Node.DOCUMENT_POSITION_FOLLOWING).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
    expect(within(nodeInspector).queryByRole("textbox", { name: "Input name" })).not.toBeInTheDocument();
    expect(within(nodeInspector).getByRole("button", { name: "Reorder input field" })).toBeInTheDocument();
    expect(within(nodeInspector).getByTestId("workflow-input-field")).toHaveClass(
      "workflow-editor-input-field",
    );

    fireEvent.click(initialFieldTitle);
    expect(within(nodeInspector).getByRole("textbox", { name: "Input name" })).toHaveValue("summary");
    fireEvent.keyDown(within(nodeInspector).getByRole("textbox", { name: "Input name" }), {
      key: "Escape",
    });
    expect(within(nodeInspector).getByRole("button", { name: "summary" })).toBeInTheDocument();

    fireEvent.click(addButton);

    const newFieldName = within(nodeInspector).getByRole("textbox", { name: "Input name" });
    expect(inputFieldNames(nodeInspector)).toEqual(["", "summary"]);
    expect(newFieldName).toHaveValue("");
    expect(within(nodeInspector).getAllByRole("button", { name: "Reorder input field" })).toHaveLength(2);
    expect(within(nodeInspector).getAllByTestId("workflow-input-field")[0]).toHaveClass(
      "workflow-editor-input-field",
    );

    fireEvent.change(newFieldName, { target: { value: "details" } });
    expect(within(nodeInspector).getByRole("textbox", { name: "Input name" })).toHaveValue("details");
    expect(inputFieldNames(nodeInspector)).toEqual(["details", "summary"]);
    fireEvent.keyDown(newFieldName, { key: "Enter" });

    const dragSurfaces = within(nodeInspector).getAllByRole("button", { name: "Reorder input field" });
    const summaryDragSurface = dragSurfaces[1] ?? dragSurfaces[0];
    summaryDragSurface?.focus();
    expect(summaryDragSurface).toHaveFocus();
    await user.keyboard("[Enter][ArrowUp][Enter]");

    await waitFor(() => {
      expect(inputFieldNames(nodeInspector)).toEqual(["summary", "details"]);
    });

    const detailsDeleteButton = within(nodeInspector).getAllByRole("button", { name: "Delete field" })[1];
    if (detailsDeleteButton === undefined) {
      throw new Error("Expected a delete button for the reordered output field.");
    }
    fireEvent.click(detailsDeleteButton);
    await waitFor(() => {
      expect(within(nodeInspector).getAllByTestId("workflow-input-field")).toHaveLength(1);
    });
    expect(inputFieldNames(nodeInspector)).toEqual(["summary"]);
  });

  it("edits join provider selections from server-derived required inputs", async () => {
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
    const providerSelect = within(nodeInspector).getByRole("combobox", { name: "summary" });

    fireEvent.click(providerSelect);
    fireEvent.click(await within(nodeInspector).findByRole("option", { name: "Implement / done" }));
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

  it("stretches workflow editor loading state content across the full island", async () => {
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(
      <App
        services={createTestServices([
          ...startupRoutes,
          { method: "workflow.get", handler: async () => new Promise(() => undefined) },
          { method: "workflow.validate", result: invalidValidationResponse },
        ])}
      />,
    );

    expect(await screen.findByTestId("loading-state")).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByTestId("loading-state")).toHaveClass("h-full", "min-h-0", "p-[var(--space-2)]");
      expect(screen.getByTestId("loading-state-island")).toHaveClass("h-full", "w-full");
      expect(screen.getByTestId("loading-state-island")).not.toHaveClass("max-w-[760px]", "m-auto");
    });
    await waitFor(() => {
      expect(screen.getByTestId("loading-state-content")).toHaveClass("w-full", "max-w-none");
    });
    expect(screen.getByTestId("loading-state-content")).not.toHaveClass("max-w-[560px]");
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
        expect(
          screen.queryByRole("complementary", { name: "Inspect workflow" }),
        ).not.toBeInTheDocument();
      });

      fireEvent.click(screen.getByRole("button", { name: "Open standard sidebar" }));
      const reopenedStandardSidebar = await screen.findByRole("complementary", { name: "Settings" });
      expect(sidebarWidthStyle(reopenedStandardSidebar)).toBe("360px");
      fireEvent.click(within(reopenedStandardSidebar).getByRole("button", { name: "Close" }));
      await waitFor(() => {
        expect(screen.queryByRole("complementary", { name: "Settings" })).not.toBeInTheDocument();
      });

      fireEvent.click(screen.getByRole("button", { name: "Inspect workflow" }));
      expect(
        sidebarWidthStyle(await screen.findByRole("complementary", { name: "Inspect workflow" })),
      ).toBe("592px");
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

    expect(screen.getAllByRole("heading").length).toBeGreaterThan(0);
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
    expect(screen.getByTestId("route-transition-frame")).not.toHaveClass("p-[var(--space-2)]");
    expect(screen.getByTestId("workflow-editor-route")).toHaveClass("fixed", "inset-0");
    expect(screen.getByTestId("workflow-editor-route")).not.toHaveClass("p-[var(--space-2)]");
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
    expect(unsavedChanges).toHaveClass(
      "max-h-[min(400px,calc(100vh-32px))]",
      "w-[min(400px,calc(100vw-32px))]",
      "overflow-y-auto",
    );
    const title = within(unsavedChanges).getByRole("heading", { name: "Unsaved changes" });
    const discardButton = within(unsavedChanges).getByRole("button", { name: "Discard" });
    const saveButton = within(unsavedChanges).getByRole("button", { name: "Save" });
    expect(discardButton).toHaveClass("w-full");
    expect(saveButton).toHaveClass("w-full");
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

    expect(unsavedChanges).toHaveClass(
      "max-h-[min(400px,calc(100vh-32px))]",
      "overflow-y-auto",
      "overflow-x-hidden",
    );
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

function inputFieldNames(container: HTMLElement): readonly string[] {
  return within(container)
    .getAllByTestId("workflow-input-field")
    .map((field) => field.dataset.inputFieldName ?? "");
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

function mockInputFieldLayout(): void {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function getBoundingClientRect(
    this: HTMLElement,
  ) {
    if (this.dataset.testid === "workflow-input-field") {
      return domRect({ height: 96, top: this.dataset.inputFieldName === "summary" ? 140 : 20, width: 320 });
    }
    return domRect({ height: 96, top: 20, width: 320 });
  });
}

function domRect({ height, top = 0, width }: Readonly<{ height: number; top?: number; width: number }>): DOMRect {
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

function CachedEdgeInspectorFixture() {
  const queryClient = useQueryClient();
  useEffect(() => {
    queryClient.setQueryData(queryKeys.workflowDefinition("workflow-1"), cachedWorkflowDefinition);
    queryClient.setQueryData(queryKeys.workflowValidation("workflow-1", "execution"), cachedValidation);
  }, [queryClient]);
  return <WorkflowInspectorSidebar selection={{ kind: "edge", edgeID: "edge-2" }} workflowID="workflow-1" />;
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
      details: { fieldName: "", inputName: "summary", placeholder: "", providerEdgeID: "edge-provider" },
      relatedIDs: [],
      blocksContext: true,
    },
  ],
};
