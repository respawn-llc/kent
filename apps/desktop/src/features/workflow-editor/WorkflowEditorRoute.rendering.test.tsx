import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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
  CachedEdgeInspectorFixture,
  CachedNodeInspectorFixture,
  OpenStandardSidebar,
} from "./workflowEditorRouteTestDrivers";
import {
  MockResizeObserver,
  mockSidebarLayout,
  mockWindowWidth,
  nativeBridgeWithClipboard,
  sidebarWidthStyle,
  workflowDraftValidationCallCount,
  workflowEditorRoutes,
} from "./workflowEditorRouteTestUtils";
import {
  activeLinkResponse,
  boardResponse,
  cachedWorkflowDefinition,
  graphValidationResponse,
  invalidValidationResponse,
  validGraphValidationResponse,
} from "./workflowEditorRouteValidationFixtures";
import {
  workflowDefinitionResponse,
} from "./workflowEditorRouteWorkflowFixtures";

describe("WorkflowEditorRoute rendering and inspectors", () => {
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
    expect(within(nodeInspector).getByRole("region", { name: "Outputs" })).toBeInTheDocument();
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


  it("does not issue project-scoped link-gate calls for a blank project context", async () => {
    // A whitespace-only projectId is not a real project context (e.g. opened from the
    // global workflow library). It must not trigger listProjectLinks/board.get, which the
    // server rejects for an empty project_id and which would fatally block the editor.
    window.history.pushState(null, "", "/workflows/workflow-1/editor?projectId=%20");
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.get", result: workflowDefinitionResponse },
      { method: "workflow.validate", result: invalidValidationResponse },
      { method: "workflow.graph.validateDraft", result: graphValidationResponse },
    ]);

    render(<App services={services} />);

    await screen.findByTestId("workflow-editor-canvas", undefined, { timeout: 5_000 });

    const calledMethods = services.transport.calls.map((call) => call.method);
    expect(calledMethods).not.toContain("workflow.listProjectLinks");
    expect(calledMethods).not.toContain("workflow.board.get");
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


  it("opens sidebar destinations with their own typed default widths", async () => {
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
      expect(sidebarWidthStyle(standardSidebar)).toBe("350px");
      fireEvent.click(within(standardSidebar).getByRole("button", { name: "Close" }));
      await waitFor(() => {
        expect(screen.queryByRole("complementary", { name: "Settings" })).not.toBeInTheDocument();
      });

      fireEvent.click(screen.getByRole("button", { name: "Inspect workflow" }));
      const workflowInspector = await screen.findByRole("complementary", { name: "Inspect workflow" });
      expect(sidebarWidthStyle(workflowInspector)).toBe("550px");
      fireEvent.keyDown(within(workflowInspector).getByRole("separator", { name: "Resize sidebar" }), {
        key: "ArrowLeft",
      });
      expect(sidebarWidthStyle(workflowInspector)).toBe("582px");
      fireEvent.click(within(workflowInspector).getByRole("button", { name: "Close" }));
      await waitFor(() => {
        expect(screen.queryByRole("complementary", { name: "Inspect workflow" })).not.toBeInTheDocument();
      });

      fireEvent.click(screen.getByRole("button", { name: "Open standard sidebar" }));
      const reopenedStandardSidebar = await screen.findByRole("complementary", { name: "Settings" });
      expect(sidebarWidthStyle(reopenedStandardSidebar)).toBe("350px");
      fireEvent.click(within(reopenedStandardSidebar).getByRole("button", { name: "Close" }));
      await waitFor(() => {
        expect(screen.queryByRole("complementary", { name: "Settings" })).not.toBeInTheDocument();
      });

      fireEvent.click(screen.getByRole("button", { name: "Inspect workflow" }));
      expect(sidebarWidthStyle(await screen.findByRole("complementary", { name: "Inspect workflow" }))).toBe(
        "582px",
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
});
