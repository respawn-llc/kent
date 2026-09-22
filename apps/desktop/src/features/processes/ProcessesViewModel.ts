import { MutationObserver, QueryObserver, type QueryClient } from "@tanstack/react-query";
import * as Cause from "effect/Cause";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import * as AsyncResult from "effect/unstable/reactivity/AsyncResult";
import * as Atom from "effect/unstable/reactivity/Atom";

import type { ApiService, ChatSessionTarget, DesktopProcess } from "@/api";
import { mutationPendingAtom, queryAtom, queryKeys } from "@/app-facade";

type ObservationState =
  Readonly<{ kind: "loading" }> | Readonly<{ kind: "ready" }> | Readonly<{ kind: "error"; error: unknown }>;

export function createProcessesViewModel({
  api,
  client,
  target,
}: Readonly<{ api: ApiService; client: QueryClient; target: ChatSessionTarget }>) {
  const queryKey = queryKeys.processes(target.projectID, target.sessionID);
  const content = queryAtom(
    new QueryObserver<readonly DesktopProcess[]>(client, {
      queryKey,
      enabled: false,
    }),
  );
  const observation = Atom.make(
    api.observeProcesses(target).pipe(
      Stream.mapEffect((processes) =>
        Effect.sync((): ObservationState => {
          client.setQueryData(queryKey, processes);
          return { kind: "ready" };
        }),
      ),
      Stream.catch((error) => Stream.succeed<ObservationState>({ kind: "error", error })),
      Stream.prepend<ObservationState>([{ kind: "loading" }]),
    ),
  );
  const state = Atom.make((get) => {
    const query = get(content);
    const result = get(observation);
    if (AsyncResult.isFailure(result)) return { kind: "error", error: Cause.squash(result.cause) } as const;
    const status = AsyncResult.isSuccess(result) ? result.value : { kind: "loading" as const };
    if (status.kind !== "ready") return status;
    if (query.data === undefined) throw new Error("Process observation requires cached contents.");
    return { kind: "ready", processes: query.data, observationTime: query.dataUpdatedAt } as const;
  });
  const retry = Atom.fn<undefined>()((_, get) =>
    Effect.sync(() => {
      get.refresh(observation);
    }),
  );
  return { state, retry } as const;
}

export function createProcessTermination({
  api,
  client,
  processID,
  onError,
}: Readonly<{ api: ApiService; client: QueryClient; processID: string; onError(error: Error): void }>) {
  const mutationKey = ["process-terminate", processID];
  const observer = new MutationObserver(client, {
    mutationKey,
    mutationFn: async () => api.killProcess(processID),
    retry: false,
    networkMode: "always",
    onError,
  });
  const request = queryAtom(observer);
  const pending = mutationPendingAtom(client, { mutationKey });
  const terminate = Atom.fn<undefined>()(
    () =>
      Effect.promise(async () => {
        if (client.isMutating({ mutationKey }) > 0) return;
        // Query's callback owns error feedback, including after the row is disposed.
        await observer.mutate().catch(() => undefined);
      }),
    { concurrent: true },
  );
  return { request, pending, terminate } as const;
}
