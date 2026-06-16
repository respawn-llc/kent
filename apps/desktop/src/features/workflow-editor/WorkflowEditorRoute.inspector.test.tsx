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
} from "./workflowEditorRouteTestDrivers";
import {
  MockResizeObserver,
  expectIdentifierInputCorrectionsDisabled,
  mockParameterLayout,
  nativeBridgeWithClipboard,
  parameterKeys,
  startupRoutesWithSubagentRoles,
  workflowDraftValidationCallCount,
} from "./workflowEditorRouteTestUtils";
import {
  graphSaveImpactResponse,
  graphValidationResponse,
  invalidValidationResponse,
  validGraphValidationResponse,
} from "./workflowEditorRouteValidationFixtures";
import {
  workflowDefinitionResponse,
  workflowDefinitionResponseWithAgentRole,
  workflowDefinitionResponseWithEdgeApproval,
  workflowDefinitionResponseWithLoopbackTarget,
  workflowDefinitionResponseWithReviewBranch,
  workflowDefinitionResponseWithRevision,
  workflowDefinitionResponseWithStartAndReviewBranch,
  workflowDefinitionResponseWithStartNode,
  workflowDefinitionResponseWithValidGroupedBranches,
} from "./workflowEditorRouteWorkflowFixtures";

describe("WorkflowEditorRoute inspector editing", () => {
  const originalResizeObserver = globalThis.ResizeObserver;

  beforeEach(() => {
    globalThis.ResizeObserver = MockResizeObserver;
  });

  afterEach(() => {
    globalThis.ResizeObserver = originalResizeObserver;
    vi.restoreAllMocks();
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
    const inspector = await screen.findByRole("complementary", { name: "Inspect transition" });
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
      "{{.Params.<transition_key>.<parameter>}}",
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
    const transitionScopedParameterChip = within(placeholders).getByRole("button", {
      name: "{{.Params.<transition_key>.<parameter>}}",
    });
    expect(transitionScopedParameterChip).toHaveAttribute("data-placeholder-tone", "primary");
    expect(within(placeholders).getByRole("button", { name: ".TaskId" })).toHaveAttribute(
      "data-placeholder-tone",
      "muted",
    );

    await user.hover(transitionScopedParameterChip);
    expect(await screen.findByTestId("transition-keyed-parameter-placeholder-help")).not.toBeEmptyDOMElement();
    await user.unhover(transitionScopedParameterChip);
    await waitFor(() => {
      expect(screen.queryByTestId("transition-keyed-parameter-placeholder-help")).not.toBeInTheDocument();
    });

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

    await user.click(transitionScopedParameterChip);
    expect(await screen.findByTestId("transition-keyed-parameter-placeholder-help")).toHaveTextContent(
      "{{.Params.planning.plan_file_location}}",
    );
    await user.keyboard("{Escape}");
    await waitFor(() => {
      expect(screen.queryByTestId("transition-keyed-parameter-placeholder-help")).not.toBeInTheDocument();
    });
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
    const inspector = await screen.findByRole("complementary", { name: "Inspect transition" });
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
    const inspector = await screen.findByRole("complementary", { name: "Inspect transition" });
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
    const inspector = await screen.findByRole("complementary", { name: "Inspect transition" });
    const edgeIDButton = within(inspector).getByRole("button", { name: "Copy transition ID edge-2" });
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
    const inspector = await screen.findByRole("complementary", { name: "Inspect transition" });
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
    const inspector = await screen.findByRole("complementary", { name: "Inspect transition" });
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
    const inspector = await screen.findByRole("complementary", { name: "Inspect transition" });

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
    const inspector = await screen.findByRole("complementary", { name: "Inspect transition" });
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
    const inspector = await screen.findByRole("complementary", { name: "Inspect transition" });
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
    const inspector = await screen.findByRole("complementary", { name: "Inspect transition" });
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
    const inspector = await screen.findByRole("complementary", { name: "Inspect transition" });

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
    const inspector = await screen.findByRole("complementary", { name: "Inspect transition" });
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
    const reloadedInspector = await screen.findByRole("complementary", { name: "Inspect transition" });
    const reloadedRouteSection = within(reloadedInspector).getByRole("region", { name: "Route" });

    expect(within(reloadedRouteSection).getByRole("checkbox", { name: "Requires approval" })).toBeChecked();
  });
});
