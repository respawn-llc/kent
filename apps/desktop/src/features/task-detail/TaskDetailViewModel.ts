import { useAtomMount, useAtomRefresh, useAtomSet, useAtomSuspense, useAtomValue } from "@effect/atom-react";
import { InfiniteQueryObserver, QueryObserver, type QueryClient } from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import { useEffect } from "react";
import type { TFunction } from "i18next";

import { errorMessage } from "@/api";
import { queryAtom, queryKeys, type AppServices, type StatusController } from "@/app-facade";
import { createUpdateTaskAction } from "@/shared/task-mutations";
import { taskDetailFeedOptions } from "./taskDetailQueries";
import { createTaskDetailEditing, sameTaskDraft } from "./TaskDetailEditing";
import { createTaskDetailCommentActions, type TaskDetailCompletion } from "./TaskDetailCommentActions";
import type { TaskDraft } from "./TaskDetailRows";
import { createTaskDetailLifecycleActions } from "./TaskDetailLifecycleActions";
import { createTaskDetailPrompts } from "./TaskDetailPrompts";
import { createTaskDetailObservation } from "./TaskDetailObservation";

export function createTaskDetailViewModel({
  services,
  client,
  taskID,
  enabled,
  retainedState,
  t,
  push,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  taskID: string;
  enabled: boolean;
  retainedState?: unknown;
  t: TFunction;
  push: StatusController["push"];
}>) {
  const active = Atom.make(enabled);
  const detailOptions = {
    queryKey: queryKeys.task(taskID),
    queryFn: async () => services.api.getTask(taskID),
  };
  const attentionOptions = {
    queryKey: queryKeys.taskAttention(taskID),
    queryFn: async () => services.api.listTaskAttention(taskID),
  };
  const activityOptions = taskDetailFeedOptions(queryKeys.activity(taskID), false, async (offset) =>
    services.api.listTaskActivity(taskID, offset),
  );
  const commentsOptions = taskDetailFeedOptions(queryKeys.comments(taskID), false, async (offset) =>
    services.api.listTaskComments(taskID, offset),
  );
  const detailObserver = new QueryObserver(client, { ...detailOptions, enabled: false });
  const attentionObserver = new QueryObserver(client, { ...attentionOptions, enabled: false });
  const activityObserver = new InfiniteQueryObserver(client, activityOptions);
  const commentsObserver = new InfiniteQueryObserver(client, commentsOptions);
  const lifetime = Atom.make((get) => {
    const enabled = get(active);
    detailObserver.setOptions({ ...detailOptions, enabled });
    attentionObserver.setOptions({ ...attentionOptions, enabled });
    activityObserver.setOptions({ ...activityOptions, enabled });
    commentsObserver.setOptions({ ...commentsOptions, enabled });
    get.addFinalizer(() => {
      detailObserver.setOptions({ ...detailOptions, enabled: false });
      attentionObserver.setOptions({ ...attentionOptions, enabled: false });
      activityObserver.setOptions({ ...activityOptions, enabled: false });
      commentsObserver.setOptions({ ...commentsOptions, enabled: false });
    });
  });
  const detailResult = queryAtom(detailObserver);
  const attentionResult = queryAtom(attentionObserver);
  const activityResult = queryAtom(activityObserver);
  const commentsResult = queryAtom(commentsObserver);
  const detail = Atom.make((get) => {
    get(lifetime);
    return get(detailResult);
  });
  const attention = Atom.make((get) => {
    get(lifetime);
    return get(attentionResult);
  });
  const activity = Atom.make((get) => {
    get(lifetime);
    return get(activityResult);
  });
  const comments = Atom.make((get) => {
    get(lifetime);
    return get(commentsResult);
  });
  const editing = createTaskDetailEditing(detail, retainedState);
  const commentActions = createTaskDetailCommentActions({
    services,
    client,
    taskID,
    editing,
    t,
    push,
    projectID: () => detailObserver.getCurrentResult().data?.projectID,
  });
  const update = createUpdateTaskAction(services.api, client);
  const lifecycle = createTaskDetailLifecycleActions({
    services,
    client,
    taskID,
    t,
    push,
    detail: () => detailObserver.getCurrentResult().data,
  });
  const prompts = createTaskDetailPrompts({
    services,
    client,
    taskID,
    t,
    push,
    attention,
    detail: () => detailObserver.getCurrentResult().data,
  });
  const reportLabelError = (error: unknown) => {
    push({
      body: errorMessage(error),
      durationMs: Infinity,
      id: "task-label-load-error",
      title: t("labels.loadFailed"),
      tone: "danger",
    });
  };
  const observation = createTaskDetailObservation({
    services,
    client,
    taskID,
    active,
    detail,
    reportLabelError,
    currentDetail: () => detailObserver.getCurrentResult().data,
  });
  const save = Atom.fn<TaskDetailCompletion & Readonly<{ draft?: TaskDraft; onSaved?: () => void }>>()(
    (input, get) =>
      Effect.gen(function* () {
        const task = detailObserver.getCurrentResult().data;
        const draft = input.draft ?? get(editing.drafts)?.draft;
        if (
          task === undefined ||
          draft === undefined ||
          get(update.request).isPending ||
          draft.title.trim().length === 0 ||
          sameTaskDraft(draft, task)
        )
          return;
        yield* get.setResult(update.submit, {
          input: { taskID, ...draft },
          projectID: task.projectID,
          onSuccess: () => input.onChanged?.(),
          onError: (error) => {
            push({
              id: "task-update-error",
              tone: "danger",
              title: t("states.error"),
              body: errorMessage(error),
            });
          },
        });
        const current = (yield* Atom.get(editing.drafts))?.draft;
        if ((yield* Atom.get(update.request)).isSuccess && sameTaskDraft(current, draft)) {
          input.onSaved?.();
        }
      }),
    { concurrent: true },
  );
  return {
    editing: editing.view,
    commentActions,
    update,
    save,
    lifecycle,
    prompts,
    observation,
    reportLabelError,
    setActive: Atom.fn<boolean>()((enabled) => Atom.set(active, enabled)),
    detail,
    attention,
    activity,
    comments,
    retryDetail: Atom.fn(() => Effect.promise(async () => detailObserver.refetch())),
    retryAttention: Atom.fn(() => Effect.promise(async () => attentionObserver.refetch())),
    retryActivity: Atom.fn(() => Effect.promise(async () => activityObserver.refetch())),
    retryComments: Atom.fn(() => Effect.promise(async () => commentsObserver.refetch())),
    nextActivity: Atom.fn(() => Effect.promise(async () => activityObserver.fetchNextPage())),
    previousActivity: Atom.fn(() => Effect.promise(async () => activityObserver.fetchPreviousPage())),
    nextComments: Atom.fn(() => Effect.promise(async () => commentsObserver.fetchNextPage())),
    previousComments: Atom.fn(() => Effect.promise(async () => commentsObserver.fetchPreviousPage())),
  } as const;
}

export type TaskDetailViewModel = ReturnType<typeof createTaskDetailViewModel>;

export function useTaskDetailObservation(model: TaskDetailViewModel) {
  const result = useAtomSuspense(model.observation);
  return { error: result.value, retry: useAtomRefresh(model.observation) };
}

export function useTaskDetailActions(model: TaskDetailViewModel) {
  useAtomMount(model.editing.state);
  useAtomMount(model.commentActions.request);
  useAtomMount(model.update.request);
  useAtomMount(model.lifecycle.interruptRequest);
  useAtomMount(model.lifecycle.approvalRequest);
  useAtomMount(model.lifecycle.deletion.request);
  useAtomMount(model.prompts.state);
  return {
    editDraft: useAtomSet(model.editing.editDraft),
    editComment: useAtomSet(model.editing.editComment),
    editNewComment: useAtomSet(model.editing.editNewComment),
    presentDescription: useAtomSet(model.editing.presentDescription),
    selectTab: useAtomSet(model.editing.selectTab),
    focusDependencies: useAtomSet(model.editing.focusDependencies),
    commentRequest: useAtomValue(model.commentActions.request),
    submitComment: useAtomSet(model.commentActions.submit),
    deleteComment: useAtomSet(model.commentActions.remove),
    update: useAtomValue(model.update.request),
    save: useAtomSet(model.save),
    interrupt: useAtomSet(model.lifecycle.interrupt),
    approve: useAtomSet(model.lifecycle.approve),
    remove: useAtomSet(model.lifecycle.remove),
    interruptRequest: useAtomValue(model.lifecycle.interruptRequest),
    approvalRequest: useAtomValue(model.lifecycle.approvalRequest),
    deletion: useAtomValue(model.lifecycle.deletion.request),
    answer: useAtomSet(model.prompts.answer),
    editSelection: useAtomSet(model.prompts.editSelection),
  };
}

export function useTaskDetailReads(model: TaskDetailViewModel, enabled: boolean) {
  const setActive = useAtomSet(model.setActive);
  useEffect(() => {
    setActive(enabled);
  }, [enabled, setActive]);
  return {
    detail: { ...useAtomValue(model.detail), refetch: useAtomSet(model.retryDetail) },
    attention: { ...useAtomValue(model.attention), refetch: useAtomSet(model.retryAttention) },
    activity: {
      ...useAtomValue(model.activity),
      refetch: useAtomSet(model.retryActivity),
      fetchNextPage: useAtomSet(model.nextActivity),
      fetchPreviousPage: useAtomSet(model.previousActivity),
    },
    comments: {
      ...useAtomValue(model.comments),
      refetch: useAtomSet(model.retryComments),
      fetchNextPage: useAtomSet(model.nextComments),
      fetchPreviousPage: useAtomSet(model.previousComments),
    },
  };
}

export type TaskDetailReads = ReturnType<typeof useTaskDetailReads>;
