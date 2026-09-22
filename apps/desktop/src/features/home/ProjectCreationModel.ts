import { useAtomMount, useAtomSet } from "@effect/atom-react";
import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import type { TFunction } from "i18next";
import { errorMessage } from "@/api";
import type { AppServices, StatusController } from "@/app-facade";
import { basename, projectKeyFromName, queryAtom, queryKeys } from "@/app-facade";
import type { ProjectDraft } from "./ProjectCreateForm";

type SelectionContext = Readonly<{
  openProject: (projectID: string) => Promise<void>;
  openDraft: (draft: ProjectDraft) => void;
}>;
type Submission = Readonly<{
  draft: ProjectDraft;
  notifyCreated?: (projectID: string) => Promise<void>;
  complete: (projectID: string, outcome: "attached" | "created") => Promise<void>;
  selectionRequired: () => void;
}>;

export function createProjectCreationModel({
  services,
  client,
  t,
  push,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  push: StatusController["push"];
  t: TFunction;
}>) {
  const selectionRequired = () => {
    push({
      id: "project-create-selection-required",
      tone: "info",
      title: t("home.workspaceSelectionRequired"),
      body: t("home.workspaceSelectionRequiredBody"),
    });
  };
  const report = (stage: "picker" | "plan" | "submit", cause: unknown) => {
    push({
      id: `project-create-${stage}-error`,
      tone: "danger",
      title: t(stage === "picker" ? "home.workspacePickerError" : "home.workspacePlanError"),
      body: errorMessage(cause),
    });
  };
  const creation = new MutationObserver(client, {
    mutationFn: async ({ draft }: Submission) =>
      services.api.createProject(draft.name.trim(), draft.key.trim().toUpperCase(), draft.workspaceRoot),
    onSuccess: async (binding, input) => {
      await client.invalidateQueries({ queryKey: queryKeys.projects });
      await input.notifyCreated?.(binding.projectID);
      await input.complete(binding.projectID, "created");
    },
    onError: (error) => {
      report("submit", error);
    },
  });
  const request = queryAtom(creation);
  const selection = Atom.fn<SelectionContext>()(
    (input) =>
      Effect.gen(function* () {
        const selected = yield* Effect.tryPromise({
          try: async () =>
            services.nativeBridge.directories.selectDirectory({ title: t("home.chooseWorkspace") }),
          catch: (cause) => ({ stage: "picker" as const, cause }),
        });
        if (selected === null) return;
        const plan = yield* Effect.tryPromise({
          try: async () => services.api.planWorkspace(selected.path),
          catch: (cause) => ({ stage: "plan" as const, cause }),
        });
        const binding = plan.binding;
        if (binding !== null) {
          yield* Effect.tryPromise({
            try: async () => input.openProject(binding.projectID),
            catch: (cause) => ({ stage: "plan" as const, cause }),
          });
          return;
        }
        if (plan.kind === "local_unbound") {
          const name = basename(plan.canonicalRoot);
          const draft = { name, key: projectKeyFromName(name), workspaceRoot: plan.canonicalRoot };
          if (!services.nativeBridge.capabilities.projectCreationWindow) {
            input.openDraft(draft);
            return;
          }
          yield* Effect.tryPromise(async () => services.nativeBridge.projectCreation.openWindow(draft)).pipe(
            Effect.catch((error) =>
              Effect.sync(() => {
                push({
                  id: "project-create-window-error",
                  tone: "danger",
                  title: t("home.projectCreateWindowError"),
                  body: errorMessage(error.cause),
                });
                input.openDraft(draft);
              }),
            ),
          );
        } else selectionRequired();
      }).pipe(
        Effect.catch((error) =>
          Effect.sync(() => {
            report(error.stage, error.cause);
          }),
        ),
      ),
    { concurrent: true },
  );
  const chooseWorkspace = Atom.fn<SelectionContext>()(
    (input, get) =>
      Effect.gen(function* () {
        if (get(selection).waiting || get(submission).waiting || creation.getCurrentResult().isPending)
          return;
        yield* get.setResult(selection, input);
      }),
    { concurrent: true },
  );
  const submission = Atom.fn<Submission>()(
    (input) =>
      Effect.gen(function* () {
        const plan = yield* Effect.tryPromise(async () =>
          services.api.planWorkspace(input.draft.workspaceRoot),
        );
        const binding = plan.binding;
        if (binding !== null) {
          const notify = input.notifyCreated;
          if (notify !== undefined) yield* Effect.tryPromise(async () => notify(binding.projectID));
          yield* Effect.tryPromise(async () => input.complete(binding.projectID, "attached"));
          return;
        }
        if (plan.kind !== "local_unbound") {
          input.selectionRequired();
          selectionRequired();
          return;
        }
        yield* Effect.tryPromise(async () => creation.mutate(input)).pipe(Effect.ignore);
      }).pipe(
        Effect.catch((error) =>
          Effect.sync(() => {
            report("submit", error.cause);
          }),
        ),
      ),
    { concurrent: true },
  );
  const submit = Atom.fn<Submission>()(
    (input, get) =>
      Effect.gen(function* () {
        if (get(selection).waiting || get(submission).waiting || creation.getCurrentResult().isPending)
          return;
        yield* get.setResult(submission, input);
      }),
    { concurrent: true },
  );
  const state = Atom.make((get) => ({
    isPending: get(selection).waiting || get(submission).waiting || get(request).isPending,
    error: get(request).error,
  }));
  return { selection, submission, request, chooseWorkspace, submit, state } as const;
}

export function useProjectCreationActions(model: ReturnType<typeof createProjectCreationModel>) {
  useAtomMount(model.selection);
  useAtomMount(model.submission);
  useAtomMount(model.request);
  return { chooseWorkspace: useAtomSet(model.chooseWorkspace), submit: useAtomSet(model.submit) };
}
