import { QueryClient } from "@tanstack/react-query";
import { RegistryProvider, useAtomMount } from "@effect/atom-react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";

import type { WorkflowProjectEvent, WorkflowProjectEventHandler } from "@/api";
import { createTestServices } from "@/test-support/app-services";
import { appI18n } from "@/i18n";
import { workflowEditorObservations } from "./workflowEditorObservation";

const fixture = vi.hoisted(() => {
  const noHandler = (): WorkflowProjectEventHandler | null => null;
  return {
    push: vi.fn(),
    workflowHandler: noHandler(),
  };
});

import { shouldNotifyWorkflowEditorRefresh, shouldRefreshWorkflowEditor } from "./workflowEditorEvents";

function workflowEvent(
  action: WorkflowProjectEvent["action"],
  workflowID = "workflow-1",
): WorkflowProjectEvent {
  return {
    action,
    occurredAtUnixMs: 1,
    primaryEntityID: workflowID,
    projectID: null,
    relatedIDs: [],
    resource: "workflow",
    workflowID,
  };
}

describe("Workflow Editor event effects", () => {
  beforeEach(() => {
    fixture.push.mockClear();
    fixture.workflowHandler = null;
  });

  it("refreshes and notifies for a matching graph save only", () => {
    const event = workflowEvent("graph_saved");

    expect(shouldRefreshWorkflowEditor(event, "project-1", "workflow-1")).toBe(true);
    expect(shouldNotifyWorkflowEditorRefresh(event, "project-1", "workflow-1")).toBe(true);
    expect(shouldRefreshWorkflowEditor(event, "project-1", "workflow-2")).toBe(false);
  });

  it("invalidates every editor owner for a matching graph save and ignores unrelated events", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const invalidation = vi.spyOn(queryClient, "invalidateQueries");
    const services = createTestServices([]);
    vi.spyOn(services.api, "subscribeWorkflow").mockImplementation((_id, handler) => {
      fixture.workflowHandler = handler;
      return { close: vi.fn() };
    });
    const observations = workflowEditorObservations({
      api: services.api,
      client: queryClient,
      projectID: "project-1",
      workflowID: "workflow-1",
      t: appI18n.t,
      push: fixture.push,
    });
    const view = renderHook(
      () => {
        useAtomMount(observations.workflow);
      },
      {
        wrapper: ({ children }: Readonly<{ children: ReactNode }>) =>
          createElement(RegistryProvider, { children }),
      },
    );
    await waitFor(() => {
      expect(fixture.workflowHandler).not.toBeNull();
    });
    invalidation.mockClear();

    act(() => {
      fixture.workflowHandler?.onEvent(workflowEvent("graph_saved"));
    });
    await waitFor(() => {
      expect(invalidation).toHaveBeenCalledTimes(5);
      expect(fixture.push).toHaveBeenCalledOnce();
    });
    const invalidatedQueryKeys = invalidation.mock.calls.map((call) => {
      const [request] = call;
      if (request === undefined) {
        throw new Error("Workflow Editor invalidated a query without a request.");
      }
      return request.queryKey;
    });
    expect(invalidatedQueryKeys).toEqual(
      expect.arrayContaining([
        ["project-workflow-links", "project-1"],
        ["board", "project-1", "workflow-1"],
        ["board-node-cards", "project-1", "workflow-1"],
        ["workflow-definition", "workflow-1"],
        ["workflow-validation", "workflow-1", "execution"],
      ]),
    );

    invalidation.mockClear();
    fixture.push.mockClear();
    act(() => {
      fixture.workflowHandler?.onEvent(workflowEvent("graph_saved", "workflow-2"));
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(invalidation).not.toHaveBeenCalled();
    expect(fixture.push).not.toHaveBeenCalled();

    act(() => {
      fixture.workflowHandler?.onEvent(workflowEvent("deleted"));
    });
    await waitFor(() => {
      expect(invalidation).toHaveBeenCalledTimes(5);
    });
    expect(fixture.push).not.toHaveBeenCalled();

    view.unmount();
    queryClient.clear();
  });

  it("refreshes a deleted Workflow without showing the normal update notice", () => {
    const event = workflowEvent("deleted");

    expect(shouldRefreshWorkflowEditor(event, "project-1", "workflow-1")).toBe(true);
    expect(shouldNotifyWorkflowEditorRefresh(event, "project-1", "workflow-1")).toBe(false);
  });
});
