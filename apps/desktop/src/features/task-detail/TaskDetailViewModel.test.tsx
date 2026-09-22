import { RegistryProvider, useAtomValue } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { appI18n } from "@/i18n";
import { createTaskDetailTestServices, taskDetailResponse } from "@/test-support/task-detail";
import { deferred } from "@/test-support/chat-runtime";
import { createTaskDetailViewModel, useTaskDetailActions, useTaskDetailReads } from "./TaskDetailViewModel";

it("admits only one comment submission when invoked twice in the same turn", async () => {
  const services = createTaskDetailTestServices(taskDetailResponse);
  const response = deferred<Awaited<ReturnType<typeof services.api.addComment>>>();
  const addComment = vi.spyOn(services.api, "addComment").mockReturnValue(response.promise);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const model = createTaskDetailViewModel({
    services,
    client,
    taskID: "task-1",
    enabled: true,
    t: appI18n.t,
    push: vi.fn(),
  });
  const view = renderHook(
    () => ({
      reads: useTaskDetailReads(model, true),
      actions: useTaskDetailActions(model),
      body: useAtomValue(model.editing.state).newCommentBody,
    }),
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  await waitFor(() => {
    expect(view.result.current.reads.detail.isSuccess).toBe(true);
  });
  await act(async () => {
    view.result.current.actions.editNewComment("New comment");
    view.result.current.actions.submitComment({});
    view.result.current.actions.submitComment({});
  });
  expect(addComment).toHaveBeenCalledTimes(1);
  await act(async () => {
    response.resolve({
      id: "comment-1",
      taskID: "task-1",
      body: "New comment",
      authorKind: "user",
      authorID: null,
      createdAt: 1,
      updatedAt: 1,
    });
  });
  await waitFor(() => {
    expect(view.result.current.body).toBe("");
  });
});

it("does not submit blank comment input", async () => {
  const services = createTaskDetailTestServices(taskDetailResponse);
  const addComment = vi.spyOn(services.api, "addComment");
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const model = createTaskDetailViewModel({
    services,
    client,
    taskID: "task-1",
    enabled: true,
    t: appI18n.t,
    push: vi.fn(),
  });
  const view = renderHook(
    () => ({
      reads: useTaskDetailReads(model, true),
      actions: useTaskDetailActions(model),
    }),
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
  await waitFor(() => {
    expect(view.result.current.reads.detail.isSuccess).toBe(true);
  });
  await act(async () => {
    view.result.current.actions.editNewComment("  \n");
    view.result.current.actions.submitComment({});
  });
  expect(addComment).not.toHaveBeenCalled();
});

it("completes an accepted comment for its originating Task without clearing the replacement Task draft", async () => {
  const services = createTaskDetailTestServices(taskDetailResponse);
  const task = await services.api.getTask("task-1");
  vi.spyOn(services.api, "getTask").mockImplementation(async (id) => ({ ...task, id }));
  const response = deferred<Awaited<ReturnType<typeof services.api.addComment>>>();
  const addComment = vi.spyOn(services.api, "addComment").mockReturnValue(response.promise);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const createModel = (taskID: string) =>
    createTaskDetailViewModel({
      services,
      client,
      taskID,
      enabled: true,
      t: appI18n.t,
      push: vi.fn(),
    });
  const view = renderHook(
    ({ model }) => ({
      reads: useTaskDetailReads(model, true),
      actions: useTaskDetailActions(model),
      body: useAtomValue(model.editing.state).newCommentBody,
    }),
    {
      initialProps: { model: createModel("task-1") },
      wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
    },
  );
  const onChanged = vi.fn();
  await waitFor(() => {
    expect(view.result.current.reads.detail.isSuccess).toBe(true);
  });
  act(() => {
    view.result.current.actions.editNewComment("Task A comment");
    view.result.current.actions.submitComment({ onChanged });
  });
  await waitFor(() => {
    expect(addComment).toHaveBeenCalledWith("task-1", "Task A comment");
  });
  view.rerender({ model: createModel("task-2") });
  await waitFor(() => {
    expect(view.result.current.reads.detail.data?.id).toBe("task-2");
  });
  act(() => {
    view.result.current.actions.editNewComment("Task B draft");
  });
  await act(async () => {
    response.resolve({
      id: "comment-1",
      taskID: "task-1",
      body: "Task A comment",
      authorKind: "user",
      authorID: null,
      createdAt: 1,
      updatedAt: 1,
    });
  });
  await waitFor(() => {
    expect(onChanged).toHaveBeenCalledOnce();
  });
  expect(view.result.current.body).toBe("Task B draft");
});
