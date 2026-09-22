import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import type { TFunction } from "i18next";
import { errorMessage } from "@/api";
import { queryAtom, type AppServices, type StatusController } from "@/app-facade";
import type { createTaskDetailEditing } from "./TaskDetailEditing";
import { refreshTaskDetail } from "./taskDetailQueries";

export type TaskDetailCompletion = Readonly<{ onChanged?: (() => void) | undefined }>;
type CommentSubmission = TaskDetailCompletion &
  Readonly<{ projectID: string }> &
  (
    | Readonly<{ kind: "create"; body: string }>
    | Readonly<{ kind: "replace"; id: string; body: string }>
    | Readonly<{ kind: "delete"; id: string }>
  );

export function createTaskDetailCommentActions({
  services,
  client,
  taskID,
  editing,
  projectID,
  t,
  push,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  taskID: string;
  editing: ReturnType<typeof createTaskDetailEditing>;
  projectID(): string | undefined;
  t: TFunction;
  push: StatusController["push"];
}>) {
  const observer = new MutationObserver(client, {
    mutationFn: async (input: CommentSubmission) => {
      switch (input.kind) {
        case "create":
          return services.api.addComment(taskID, input.body);
        case "replace":
          return services.api.replaceComment(input.id, input.body);
        case "delete":
          return services.api.deleteComment(input.id);
      }
    },
    onSuccess: async (_result, input) => {
      await refreshTaskDetail(client, taskID, input.projectID);
      input.onChanged?.();
    },
    onError: (error, input) => {
      push({
        id: input.kind === "delete" ? "task-comment-delete-error" : "task-comment-save-error",
        tone: "danger",
        title: t(input.kind === "delete" ? "task.commentDeleteFailed" : "task.commentSaveFailed"),
        body: errorMessage(error),
      });
    },
  });
  const request = queryAtom(observer);
  const completeEditor = Effect.fn("TaskDetail.completeCommentEditor")(function* (
    input: Exclude<CommentSubmission, { kind: "delete" }>,
  ) {
    if (input.kind === "create") {
      if ((yield* Atom.get(editing.newCommentBody)) === input.body) {
        yield* Atom.set(editing.newCommentBody, "");
      }
    } else {
      const current = yield* Atom.get(editing.editingComment);
      if (current?.id === input.id && current.body === input.body) {
        yield* Atom.set(editing.editingComment, null);
      }
    }
  });
  const submit = Atom.fn<TaskDetailCompletion>()(
    (completion) =>
      Effect.gen(function* () {
        const project = projectID();
        if (project === undefined || observer.getCurrentResult().isPending) return;
        const editor = yield* Atom.get(editing.editingComment);
        const body = editor?.body ?? (yield* Atom.get(editing.newCommentBody));
        if (body.trim().length === 0) return;
        const input: CommentSubmission =
          editor === null
            ? { ...completion, projectID: project, kind: "create", body }
            : { ...completion, projectID: project, kind: "replace", id: editor.id, body };
        const result = yield* Effect.tryPromise(async () => observer.mutate(input)).pipe(Effect.result);
        if (result._tag === "Failure") return;
        yield* completeEditor(input);
      }),
    { concurrent: true },
  );
  const remove = Atom.fn<TaskDetailCompletion & Readonly<{ id: string }>>()(
    (input) =>
      Effect.gen(function* () {
        const project = projectID();
        if (project === undefined || observer.getCurrentResult().isPending) return;
        yield* Effect.tryPromise(async () =>
          observer.mutate({
            ...input,
            projectID: project,
            kind: "delete",
          }),
        ).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  return { request, submit, remove } as const;
}
