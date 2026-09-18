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
export type ComposerDraftValue = Readonly<{ text: string; protectedInput: string | null }>;
type EditorValue = ComposerDraftValue & Readonly<{ kind: "opening" | "editing" }>;
export type ComposerHistoryMovement =
  | Readonly<{ kind: "none" | "blocked" }>
  | Readonly<{ kind: "replaced" | "restored"; cursor: "start" | "end" }>;
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
  const editor = Atom.make<EditorValue>({ kind: "opening", text: "", protectedInput: null });
  const selection = Atom.make<number | null>(null);
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
      if (opening.kind === "session") {
        const initial = await services.api.chat.getDraft(opening);
        return { text: initial.input, protectedInput: initial.protectedInput };
      }
      const stored = readBrowserStorage("local", newChatDraftKey);
      if (stored.ok) return { text: stored.value ?? "", protectedInput: null };
      reportStorageFailure(stored.error);
      return { text: "", protectedInput: null };
    },
    ...composerReadOptions,
    staleTime: Infinity,
  });
  const read = queryAtom(observer);
  const value = Atom.make<ComposerDraftValue>((get) => {
    const local = get(editor);
    const initial = get(read);
    return local.kind === "opening" && initial.isSuccess
      ? {
          text: mergeComposerText(local.text, initial.data.text, "prepend"),
          protectedInput: initial.data.protectedInput,
        }
      : { text: local.text, protectedInput: local.protectedInput };
  });
  const text = Atom.make((get) => get(value).text);
  const saveKey = ["chat-draft-save", crypto.randomUUID()];
  const saveOptions = {
    ...composerRequestOptions,
    scope: { id: crypto.randomUUID() },
    mutationFn: async ({
      target,
      input,
    }: Readonly<{ target: ChatSessionTarget; input: ComposerDraftValue }>) =>
      services.api.chat.persistDraft(target, input.text, input.protectedInput),
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
    input: ComposerDraftValue,
    intent: "editing" | "navigation" = "editing",
  ) {
    saveObserver.setOptions({ ...saveOptions, mutationKey: [...saveKey, intent] });
    return saveObserver.mutate({ target, input });
  }
  const saving = queryAtom(saveObserver);
  const edit = Atom.fn<string>()(
    (input, get) =>
      Effect.sync(() => {
        if (input !== get(text)) get.set(selection, null);
        get.set(editor, {
          ...get(value),
          kind: observer.getCurrentResult().isSuccess ? "editing" : "opening",
          text: input,
        });
      }),
    { concurrent: true },
  );
  const restore = Atom.fn<ComposerTextRestoration>()(
    (input, get) =>
      Effect.sync(() => {
        const restored = mergeComposerText(get(text), input.text, input.direction);
        if (restored !== get(text)) get.set(selection, null);
        get.set(editor, {
          ...get(value),
          kind: observer.getCurrentResult().isSuccess ? "editing" : "opening",
          text: restored,
        });
      }),
    { concurrent: true },
  );
  const navigate = Atom.fn<Readonly<{ direction: -1 | 1; entries: readonly string[] }>>()((input, get) =>
    Effect.sync((): ComposerHistoryMovement => {
      if (!observer.getCurrentResult().isSuccess) return { kind: "none" };
      const current = get(value);
      const selected = get(selection);
      if (selected === null && current.protectedInput !== null && current.text !== "")
        return { kind: "blocked" };
      if (input.direction === 1 && restoresHistoryDraft(current, selected, input.entries.length)) {
        get.set(editor, { kind: "editing", text: current.protectedInput ?? "", protectedInput: null });
        get.set(selection, null);
        return { kind: "restored", cursor: "start" };
      }
      const next = nextHistoryEntry(input.entries, selected, input.direction);
      if (next === null) return { kind: "none" };
      get.set(editor, {
        kind: "editing",
        text: next.text,
        protectedInput: selected === null && current.text !== "" ? current.text : current.protectedInput,
      });
      get.set(selection, next.index);
      return { kind: "replaced", cursor: input.direction === -1 ? "start" : "end" };
    }),
  );
  const reindexHistory = Atom.fn<
    | Readonly<{ kind: "replace"; entries: readonly string[]; previous: readonly string[] }>
    | Readonly<{ kind: "trim"; removed: number }>
  >()(
    (change, get) =>
      Effect.sync(() => {
        const selected = get(selection);
        if (change.kind === "replace") {
          if (selected !== null)
            get.set(selection, replacementHistoryIndex(change.previous, change.entries, selected, get(text)));
          return;
        }
        get.set(selection, selected === null || selected < change.removed ? null : selected - change.removed);
      }),
    { concurrent: true },
  );
  const save = Atom.fn<ComposerDraftValue>()(
    (input, get) =>
      Effect.gen(function* () {
        if (!observer.getCurrentResult().isSuccess || get(persistence) === "destination-owned") return;
        const current = get(value);
        if (input.text !== current.text || input.protectedInput !== current.protectedInput) return;
        const captured = get(target);
        if (captured.kind === "new_chat") {
          persistLocal(input.text);
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
        const input = get(value);
        get.set(editor, { ...input, kind: "editing" });
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
        get.set(selection, null);
        get.set(editor, { ...get(value), kind: "editing", text: "" });
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
        const input = get(value);
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
    value,
    selection,
    navigate,
    reindexHistory,
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

function restoresHistoryDraft(value: ComposerDraftValue, selection: number | null, count: number) {
  return (
    (selection !== null && selection === count - 1) || (value.text === "" && value.protectedInput !== null)
  );
}

function nextHistoryEntry(entries: readonly string[], selected: number | null, direction: -1 | 1) {
  if (selected === null && direction === 1) return null;
  const index = selected === null ? entries.length - 1 : selected + direction;
  const text = entries[index];
  return text === undefined ? null : { index, text };
}

function replacementHistoryIndex(
  previous: readonly string[],
  entries: readonly string[],
  selected: number,
  text: string,
): number | null {
  // Count equal prompts from the newest end so an older prefix does not
  // move browsing to an older duplicate of the locally recalled prompt.
  let occurrence = previous.slice(selected).filter((entry) => entry === text).length;
  for (let index = entries.length - 1; index >= 0; index--) {
    if (entries[index] !== text) continue;
    occurrence--;
    if (occurrence === 0) return index;
  }
  return null;
}

export function useComposerDraftActions(model: ComposerDraftViewModel) {
  useAtomMount(model.read);
  useAtomMount(model.saving);
  useAtomMount(model.selection);
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
