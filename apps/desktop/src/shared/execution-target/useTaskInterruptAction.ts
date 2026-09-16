import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import { useAppServices } from "@/app-facade";
import { createTaskRequests } from "./taskRequests";

export function useTaskInterruptAction(onSuccess: () => Promise<void>) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const model = useMemo(() => {
    const requests = createTaskRequests(client);
    const execute = Atom.fn<
      Readonly<{ taskID: string; onSuccess(): Promise<void>; onError(error: unknown): void }>
    >()(
      (input) =>
        Effect.gen(function* () {
          const operation = requests.start(input.taskID, "interrupt", {
            mutationFn: async () => api.interruptTask(input.taskID),
            onSuccess: input.onSuccess,
            onError: input.onError,
          });
          if (operation !== null) yield* Effect.tryPromise(async () => operation).pipe(Effect.ignore);
        }),
      { concurrent: true },
    );
    return { pending: requests.pending, execute } as const;
  }, [api, client]);
  useAtomMount(model.pending);
  const execute = useAtomSet(model.execute, { mode: "value" });
  return {
    pendingTaskIDs: useAtomValue(model.pending).interrupt,
    execute: (taskID: string, onError: (error: unknown) => void) => {
      execute({ taskID, onSuccess, onError });
    },
  };
}
