import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import type { ApiService, WorkflowDeleteImpact } from "@/api";
import { queryAtom, queryKeys } from "@/app-facade";
import { workflowDeleteInputFromImpact } from "./workflowDeleteShared";

type PreviewInput = Readonly<{ onError(error: unknown, retry: () => void): void }>;
type DeleteInput = Readonly<{ impact: WorkflowDeleteImpact; onCompleted(): Promise<void> }>;

export function createWorkflowDeleteModel({
  api,
  client,
  workflowID,
  onCompletionError,
}: Readonly<{
  api: ApiService;
  client: QueryClient;
  workflowID: string;
  onCompletionError(error: unknown): void;
}>) {
  const preview = new MutationObserver(client, {
    mutationFn: async () => api.previewWorkflowDelete(workflowID),
  });
  const deletion = new MutationObserver(client, {
    mutationFn: async (input: DeleteInput) => api.deleteWorkflow(workflowDeleteInputFromImpact(input.impact)),
    async onSuccess(response, input) {
      if (!response.deleted) return;
      try {
        await invalidateWorkflowDeleteQueries(client, workflowID);
        await input.onCompleted();
      } catch (error) {
        onCompletionError(error);
      }
    },
  });
  const open: Atom.AtomResultFn<PreviewInput, void> = Atom.fn<PreviewInput>()(
    (input, get) =>
      Effect.gen(function* () {
        const current = deletion.getCurrentResult();
        const read = preview.getCurrentResult();
        if (read.isPending || read.data !== undefined || current.isPending || current.data?.deleted === true)
          return;
        yield* Effect.tryPromise(async () => preview.mutate()).pipe(
          Effect.match({
            onSuccess: () => undefined,
            onFailure: (error) => {
              input.onError(error.cause, () => {
                get.set(open, input);
              });
            },
          }),
        );
      }),
    { concurrent: true },
  );
  const confirm = Atom.fn<Readonly<{ onCompleted(): Promise<void> }>>()(
    (input) =>
      Effect.gen(function* () {
        const impact = preview.getCurrentResult().data;
        const current = deletion.getCurrentResult();
        if (impact === undefined || current.isPending || current.data?.deleted === true) return;
        yield* Effect.tryPromise(async () =>
          deletion.mutate({ impact, onCompleted: input.onCompleted }),
        ).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const cancel = Atom.fn(() =>
    Effect.sync(() => {
      if (!deletion.getCurrentResult().isPending) {
        preview.reset();
        deletion.reset();
      }
    }),
  );
  return { preview: queryAtom(preview), deletion: queryAtom(deletion), open, confirm, cancel } as const;
}

async function invalidateWorkflowDeleteQueries(client: QueryClient, workflowID: string): Promise<void> {
  client.removeQueries({ queryKey: queryKeys.workflowDefinition(workflowID) });
  client.removeQueries({ queryKey: queryKeys.workflowValidation(workflowID, "execution") });
  await Promise.all([
    client.invalidateQueries({ queryKey: queryKeys.allWorkflows }),
    client.invalidateQueries({ queryKey: queryKeys.allWorkflowDefinitions }),
    client.invalidateQueries({ queryKey: queryKeys.allWorkflowValidations }),
    client.invalidateQueries({ queryKey: queryKeys.allWorkflowGraphLayouts }),
    client.invalidateQueries({ queryKey: queryKeys.allProjectWorkflowLinks }),
    client.invalidateQueries({ queryKey: queryKeys.projects }),
    client.invalidateQueries({ queryKey: queryKeys.allBoards }),
    client.invalidateQueries({ queryKey: queryKeys.allAttention }),
    client.invalidateQueries({ queryKey: queryKeys.allTasks }),
  ]);
}
