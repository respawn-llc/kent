import { MutationObserver, QueryObserver, type QueryClient } from "@tanstack/react-query";
import { useAtomMount, useAtomSet } from "@effect/atom-react";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import type { TFunction } from "i18next";

import { errorMessage, type ChatSettingsTarget, type ChatSessionTarget } from "@/api";
import {
  queryAtom,
  mutationPendingAtom,
  readBrowserStorage,
  recoverOrThrowDebugFailure,
  writeBrowserStorage,
  type AppServices,
} from "@/app-facade";
import { showStatusToast } from "@/ui";
import { mergeComposerText } from "./composerText";

export const composerRequestOptions = { retry: false, networkMode: "always" } as const;
export const composerReadOptions = {
  ...composerRequestOptions,
  refetchOnWindowFocus: false,
  refetchOnReconnect: false,
} as const;
const newChatDraftKey = "desktop.newChatDraft";
type EditorValue = Readonly<{ kind: "opening" | "editing"; text: string }>;
export type ComposerTextRestoration = Readonly<{ text: string; direction: "append" | "prepend" }>;

export function createComposerDraftViewModel({
  services,
  client,
  target,
  opening,
  t,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  target: Atom.Atom<ChatSettingsTarget>;
  opening: ChatSettingsTarget;
  t: TFunction;
}>) {
  const editor = Atom.make<EditorValue>({ kind: "opening", text: "" });
  const persistence = Atom.make<"editing" | "destination-owned">("editing");
  const reportStorageFailure = (error: unknown) => {
    void recoverOrThrowDebugFailure({
      error,
      logger: services.logger,
      message: "New Chat draft storage failed.",
      context: {},
      recover: () => {
        /* Local storage is best-effort; keep the unsent editor. */
      },
    });
  };
  function persistLocal(input: string) {
    const saved = writeBrowserStorage("local", newChatDraftKey, input);
    if (!saved.ok) reportStorageFailure(saved.error);
  }
  const observer = new QueryObserver(client, {
    queryKey: ["chat-composer-draft", crypto.randomUUID()],
    queryFn: async () => {
      if (opening.kind === "session") return services.api.chat.getDraft(opening);
      const stored = readBrowserStorage("local", newChatDraftKey);
      if (stored.ok) return stored.value ?? "";
      reportStorageFailure(stored.error);
      return "";
    },
    ...composerReadOptions,
    staleTime: Infinity,
  });
  const read = queryAtom(observer);
  const text = Atom.make((get) => {
    const local = get(editor);
    const initial = get(read);
    return local.kind === "opening" && initial.isSuccess
      ? mergeComposerText(local.text, initial.data, "prepend")
      : local.text;
  });
  const saveKey = ["chat-draft-save", crypto.randomUUID()];
  const saveOptions = {
    ...composerRequestOptions,
    scope: { id: crypto.randomUUID() },
    mutationFn: async ({ target, input }: Readonly<{ target: ChatSessionTarget; input: string }>) =>
      services.api.chat.persistDraft(target, input),
    onError: (error: Error) => {
      showStatusToast({
        id: "chat-composer-draft",
        tone: "danger",
        title: t("chatComposer.saveFailed"),
        body: errorMessage(error),
      });
    },
  };
  const saveObserver = new MutationObserver(client, saveOptions);
  const navigationPending = mutationPendingAtom(client, { mutationKey: [...saveKey, "navigation"] });
  async function persist(
    target: ChatSessionTarget,
    input: string,
    intent: "editing" | "navigation" = "editing",
  ) {
    saveObserver.setOptions({ ...saveOptions, mutationKey: [...saveKey, intent] });
    return saveObserver.mutate({ target, input });
  }
  const saving = queryAtom(saveObserver);
  const edit = Atom.fn<string>()(
    (value, get) =>
      Effect.sync(() => {
        get.set(editor, { kind: observer.getCurrentResult().isSuccess ? "editing" : "opening", text: value });
      }),
    { concurrent: true },
  );
  const restore = Atom.fn<ComposerTextRestoration>()(
    (input, get) =>
      Effect.sync(() => {
        get.set(editor, {
          kind: observer.getCurrentResult().isSuccess ? "editing" : "opening",
          text: mergeComposerText(get(text), input.text, input.direction),
        });
      }),
    { concurrent: true },
  );
  const save = Atom.fn<string>()(
    (input, get) =>
      Effect.gen(function* () {
        if (!observer.getCurrentResult().isSuccess || get(persistence) === "destination-owned") return;
        if (input !== get(text)) return;
        const captured = get(target);
        if (captured.kind === "new_chat") {
          persistLocal(input);
          return;
        }
        yield* Effect.tryPromise(async () => persist(captured, input)).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const retry = Atom.fn(() => Effect.promise(async () => observer.refetch()), { concurrent: true });
  const begin = Atom.fn<undefined>()(
    (_, get) =>
      Effect.sync(() => {
        if (get(target).kind !== "new_chat") return;
        persistLocal(get(text));
        get.set(persistence, "destination-owned");
      }),
    { concurrent: true },
  );
  const resume = Atom.fn<undefined>()(
    (_, get) =>
      Effect.sync(() => {
        if (get(target).kind !== "new_chat") return;
        get.set(persistence, "editing");
        persistLocal(get(text));
      }),
    { concurrent: true },
  );
  const adopt = Atom.fn<ChatSessionTarget>()(
    (session, get) =>
      Effect.gen(function* () {
        const input = get(text);
        get.set(editor, { kind: "editing", text: input });
        get.set(persistence, "editing");
        persistLocal("");
        yield* Effect.tryPromise(async () => persist(session, input)).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const submit = Atom.fn<undefined>()(
    (_, get) =>
      Effect.gen(function* () {
        yield* get.setResult(begin, undefined);
        get.set(editor, { kind: "editing", text: "" });
      }),
    { concurrent: true },
  );
  const flush = Atom.fn<undefined>()(
    (_, get) =>
      Effect.gen(function* () {
        const selected = get(target);
        if (selected.kind === "new_chat") {
          if (get(persistence) === "editing" && observer.getCurrentResult().isSuccess)
            persistLocal(get(text));
          return true;
        }
        if (!observer.getCurrentResult().isSuccess) {
          const local = get(editor);
          if (
            observer.getCurrentResult().data === undefined &&
            local.kind === "opening" &&
            local.text.length === 0
          )
            return true;
          const initial = yield* Effect.promise(async () => observer.refetch());
          if (!initial.isSuccess) return false;
        }
        const input = get(text);
        return yield* Effect.tryPromise(async () => persist(selected, input, "navigation")).pipe(
          Effect.as(true),
          Effect.orElseSucceed(() => false),
        );
      }),
    { concurrent: true },
  );
  return {
    read,
    text,
    saving,
    edit,
    restore,
    save,
    retry,
    submit,
    begin,
    resume,
    adopt,
    flush,
    navigationPending,
  } as const;
}

export type ComposerDraftViewModel = ReturnType<typeof createComposerDraftViewModel>;
export function useComposerDraftActions(model: ComposerDraftViewModel) {
  useAtomMount(model.read);
  useAtomMount(model.saving);
  return {
    edit: useAtomSet(model.edit),
    restore: useAtomSet(model.restore),
    save: useAtomSet(model.save),
    retry: useAtomSet(model.retry),
    submit: useAtomSet(model.submit),
    begin: useAtomSet(model.begin),
    resume: useAtomSet(model.resume),
    adopt: useAtomSet(model.adopt),
    flush: useAtomSet(model.flush, { mode: "promise" }),
  };
}
