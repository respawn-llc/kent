import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "bun:test";
import { QueryClient } from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import { z } from "zod";
import type { ProjectObservation } from "@/api";
import { queryKeys, useAppNavigation } from "@/app-facade";
import { ProjectLabelsProvider } from "@/shared/labels";
import { TestAppProviders, createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { useBoardDeleteConfirmation, useBoardTaskDeletions } from "./BoardTaskDeletion";
import { useProjectBoardSubscription } from "./useBoardData";

function fixture() {
  const services = createTestServices([]);
  const client = new QueryClient();
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const response = deferred<undefined>();
  const deleted = deferred<ProjectObservation>();
  const remove = vi.spyOn(services.api, "deleteTask").mockReturnValue(response.promise);
  vi.spyOn(services.api, "subscribeProject").mockReturnValue(
    Stream.concat(Stream.fromEffect(Effect.promise(async () => deleted.promise)), Stream.never),
  );
  const root = createRootRoute({
    component: () => (
      <TestAppProviders services={services} queryClient={client}>
        <Outlet />
      </TestAppProviders>
    ),
  });
  const board = createRoute({
    getParentRoute: () => root,
    path: "/projects/$projectId",
    validateSearch: z.object({ taskId: z.string().optional() }),
    component: () => (
      <ProjectLabelsProvider projectID="project-1" queryEnabled={false} subscribeToProject={false}>
        <Board />
      </ProjectLabelsProvider>
    ),
  });
  function Board() {
    const { taskId } = board.useSearch();
    const navigation = useAppNavigation();
    const deletions = useBoardTaskDeletions();
    const deletion = useBoardDeleteConfirmation({
      deletions,
      projectID: "project-1",
      onError: vi.fn(),
    });
    useProjectBoardSubscription("project-1", "workflow-1", {
      selectedWorkflowID: "workflow-1",
      selectedTaskID: taskId,
      onSelectedTaskDeleted: () => {
        void navigation.closeProjectTask("project-1", "workflow-1");
      },
    });
    return (
      <button
        onClick={() => {
          deletion.confirm("task-a");
        }}
      >
        Delete A
      </button>
    );
  }
  const home = createRoute({ getParentRoute: () => root, path: "/", component: () => <div>Home</div> });
  const router = createRouter({
    routeTree: root.addChildren([board, home]),
    history: createMemoryHistory({ initialEntries: ["/projects/project-1?taskId=task-a"] }),
  });
  render(<RouterProvider router={router} />);
  return { router, response, deleted, remove, invalidate };
}

const deletionEvent: ProjectObservation = {
  kind: "event",
  event: {
    resource: "task",
    action: "deleted",
    primaryEntityID: "task-a",
    projectID: "project-1",
    workflowID: "workflow-1",
    relatedIDs: [],
    occurredAtUnixMs: 1,
  },
};

it.each(["task-b", null])("keeps the current destination after Delete completes from %s", async (next) => {
  const view = fixture();
  fireEvent.click(await screen.findByRole("button", { name: "Delete A" }));
  await waitFor(() => {
    expect(view.remove).toHaveBeenCalledTimes(1);
  });
  await act(async () => {
    if (next === null) await view.router.navigate({ to: "/" });
    else
      await view.router.navigate({
        to: "/projects/$projectId",
        params: { projectId: "project-1" },
        search: { taskId: next },
      });
  });
  if (next === null) await screen.findByText("Home");
  await act(async () => {
    view.deleted.resolve(deletionEvent);
    view.response.resolve(undefined);
  });
  await waitFor(() => {
    expect(view.invalidate).toHaveBeenCalledWith(
      expect.objectContaining({
        queryKey: queryKeys.task("task-a"),
      }),
    );
  });
  expect(view.router.state.location.pathname).toBe(next === null ? "/" : "/projects/project-1");
  expect(view.router.state.location.search).toEqual(next === null ? {} : { taskId: "task-b" });
});

it("closes the currently selected Task from the mounted Board deletion observation", async () => {
  const view = fixture();
  fireEvent.click(await screen.findByRole("button", { name: "Delete A" }));
  await waitFor(() => {
    expect(view.remove).toHaveBeenCalledTimes(1);
  });
  await act(async () => {
    view.deleted.resolve(deletionEvent);
    view.response.resolve(undefined);
  });
  await waitFor(() => {
    expect(view.router.state.location.search).toMatchObject({ taskId: "" });
  });
});
