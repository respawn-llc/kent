import { MutationObserver, QueryObserver, type QueryClient } from "@tanstack/react-query";
import { useAtomMount, useAtomSet } from "@effect/atom-react";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import type { TFunction } from "i18next";

import { errorMessage, type ChatSettingsTarget } from "@/api";
import {
  queryAtom,
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
  t,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  target: ChatSettingsTarget;
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
  const observer = new QueryObserver(client, {
    queryKey: ["chat-composer-draft", crypto.randomUUID()],
    queryFn: async () => {
      if (target.kind === "session") return services.api.chat.getDraft(target);
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
  const saveObserver = new MutationObserver(client, {
    ...composerRequestOptions,
    mutationFn: async (input: string) => {
      if (target.kind === "session") return services.api.chat.persistDraft(target, input);
      const saved = writeBrowserStorage("local", newChatDraftKey, input);
      if (!saved.ok) reportStorageFailure(saved.error);
    },
    onError: (error) => {
      showStatusToast({
        id: "chat-composer-draft",
        tone: "danger",
        title: t("chatComposer.saveFailed"),
        body: errorMessage(error),
      });
    },
  });
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
        yield* Effect.tryPromise(async () => saveObserver.mutate(input)).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const retry = Atom.fn(() => Effect.promise(async () => observer.refetch()), { concurrent: true });
  const submit = Atom.fn<undefined>()(
    (_, get) =>
      Effect.sync(() => {
        if (target.kind === "new_chat") get.set(persistence, "destination-owned");
        get.set(editor, { kind: "editing", text: "" });
      }),
    { concurrent: true },
  );
  return { read, text, saving, edit, restore, save, retry, submit } as const;
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
  };
}
