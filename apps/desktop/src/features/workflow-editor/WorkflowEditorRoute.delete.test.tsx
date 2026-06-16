import {
  type NativeDialogWindowOptions,
  type NativeWorkflowDeleted,
} from "@app/native-bridge";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";

import { App } from "../../App";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";
import {
  MockResizeObserver,
  nativeWorkflowDeleteWindowBridge,
  nativeWorkflowEntityDeleteDialogBridge,
} from "./workflowEditorRouteTestUtils";
import {
  graphValidationResponse,
  invalidValidationResponse,
  workflowDeletePreviewResponse,
  workflowDeleteResponse,
} from "./workflowEditorRouteValidationFixtures";
import {
  workflowDefinitionResponse,
} from "./workflowEditorRouteWorkflowFixtures";

describe("WorkflowEditorRoute workflow deletion", () => {
  const originalResizeObserver = globalThis.ResizeObserver;

  beforeEach(() => {
    globalThis.ResizeObserver = MockResizeObserver;
  });

  afterEach(() => {
    globalThis.ResizeObserver = originalResizeObserver;
    vi.restoreAllMocks();
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
});
