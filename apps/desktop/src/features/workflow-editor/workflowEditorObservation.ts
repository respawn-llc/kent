import * as Effect from "effect/Effect";
import * as Cause from "effect/Cause";
import * as Queue from "effect/Queue";
import * as Stream from "effect/Stream";
import * as Atom from "effect/unstable/reactivity/Atom";
import type { QueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { errorMessage } from "@/api";
import { queryKeys, type AppServices, type StatusController } from "@/app-facade";
import {
  shouldNotifyWorkflowEditorRefresh,
  shouldRefreshWorkflowDefinition,
  shouldRefreshWorkflowLink,
} from "./workflowEditorEvents";

export function workflowEditorObservations({
  api,
  client,
  workflowID,
  projectID,
  t,
  push,
}: Readonly<{
  api: AppServices["api"];
  client: QueryClient;
  workflowID: string;
  projectID: string | null;
  t: TFunction;
  push: StatusController["push"];
}>) {
  const refresh = Effect.fn("WorkflowEditor.refresh")(function* (notify: boolean) {
    yield* Effect.tryPromise({
      try: async () => refreshWorkflowEditor(client, projectID, workflowID),
      catch: (cause) => new WorkflowEditorObservationFailure(cause),
    });
    if (notify) push({ id: "workflow-editor-updated", tone: "neutral", title: t("workflowEditor.updated") });
  });
  const workflowStream = Stream.callback<boolean, Error>(
    (queue) =>
      Effect.acquireRelease(
        Effect.try({
          try: () =>
            api.subscribeWorkflow(workflowID, {
              onOpen: () => {
                Queue.offerUnsafe(queue, false);
              },
              onEvent: (event) => {
                if (shouldRefreshWorkflowDefinition(event, workflowID)) {
                  Queue.offerUnsafe(queue, shouldNotifyWorkflowEditorRefresh(event, projectID, workflowID));
                }
              },
              onError: (error) => {
                Queue.failCauseUnsafe(queue, Cause.fail(error));
              },
              onComplete: () => {
                Queue.failCauseUnsafe(queue, Cause.fail(Cause.Done()));
              },
            }),
          catch: (cause) => new WorkflowEditorObservationFailure(cause),
        }),
        (subscription) =>
          Effect.sync(() => {
            subscription.close();
          }),
      ).pipe(Effect.catch((error) => Queue.fail(queue, error))),
    { bufferSize: 1, strategy: "sliding" },
  ).pipe(Stream.mapEffect(refresh));
  const projectStream =
    projectID === null
      ? Stream.empty
      : api.subscribeProject(projectID).pipe(
          Stream.filter(
            (observation) =>
              observation.kind !== "event" ||
              shouldRefreshWorkflowLink(observation.event, projectID, workflowID),
          ),
          Stream.takeUntil((observation) => observation.kind === "error" || observation.kind === "complete"),
          Stream.mapEffect((observation) => {
            if (observation.kind === "error") return Effect.fail(observation.error);
            if (observation.kind === "open") return refresh(false);
            if (observation.kind === "event")
              return refresh(shouldNotifyWorkflowEditorRefresh(observation.event, projectID, workflowID));
            return Effect.void;
          }),
        );
  const errorAtom = (stream: Stream.Stream<void, Error>) =>
    Atom.make(
      stream.pipe(
        Stream.map((): Error | null => null),
        Stream.catch((error) => Stream.succeed(error)),
        Stream.prepend([null]),
      ),
      { initialValue: null },
    );
  return { workflow: errorAtom(workflowStream), project: errorAtom(projectStream) };
}

class WorkflowEditorObservationFailure extends Error {
  readonly _tag = "WorkflowEditorObservationFailure";
  constructor(cause: unknown) {
    super(errorMessage(cause), { cause });
  }
}

export async function refreshWorkflowEditor(
  client: QueryClient,
  projectID: string | null,
  workflowID: string,
) {
  await Promise.all([
    ...(projectID === null
      ? []
      : [
          client.invalidateQueries({ queryKey: queryKeys.projectWorkflowLinks(projectID) }),
          client.invalidateQueries({ queryKey: queryKeys.boardWorkflowRoot(projectID, workflowID) }),
          client.invalidateQueries({ queryKey: queryKeys.boardNodeCardsWorkflowRoot(projectID, workflowID) }),
        ]),
    client.invalidateQueries({ queryKey: queryKeys.workflowDefinition(workflowID) }),
    client.invalidateQueries({ queryKey: queryKeys.workflowValidation(workflowID, "execution") }),
  ]);
}
