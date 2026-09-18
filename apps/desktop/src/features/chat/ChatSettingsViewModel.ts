import { MutationObserver, QueryObserver, skipToken, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import type { TFunction } from "i18next";
import {
  ContractError,
  type ChatContext,
  type ChatSettingsTarget,
  type ChatSettingsRead,
  type ChatSettingsMutation,
  type ChatSettingsMutationResponse,
} from "@/api";
import { queryAtom, type AppServices } from "@/app-facade";
import { showStatusToast } from "@/ui";
import { chatOperationFailureMessage } from "./chatSettingsPresentation";
import { loadedState, loadingState, settingsReducer, type SettingsState } from "./chatSettingsState";
import { composerReadOptions, composerRequestOptions } from "./ComposerDraftViewModel";

export function createChatSettingsViewModel({
  services,
  client,
  target,
  t,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  target: Atom.Atom<ChatSettingsTarget | null>;
  t: TFunction;
}>) {
  const report = (body: string) => {
    showStatusToast({
      id: "chat-settings-operation",
      tone: "danger",
      title: t("chatSettings.operationFailed"),
      body,
    });
  };
  const current = Atom.make((get) => {
    const selected = get(target);
    const local = Atom.make<Readonly<{ source: ChatSettingsRead; state: SettingsState }> | null>(null);
    const observer = new QueryObserver(client, {
      ...composerReadOptions,
      queryKey: ["chat-settings", crypto.randomUUID()],
      staleTime: Infinity,
      queryFn:
        selected === null
          ? skipToken
          : async () => {
              const result = await services.api.chat.getSettings(selected);
              if (result.kind !== selected.kind)
                throw new ContractError("Chat Settings returned a different target kind.");
              return result;
            },
    });
    const read = queryAtom(observer);
    const state = Atom.make((get): SettingsState => {
      const result = get(read);
      const edit = get(local);
      if (result.isError)
        return {
          kind: selected?.kind !== "session" ? "failed-new-chat" : "failed-session",
          error: result.error,
        };
      if (result.data !== undefined)
        return edit?.source === result.data ? edit.state : loadedState(result.data);
      return loadingState(selected?.kind ?? "new_chat");
    });
    return {
      selected,
      local,
      observer,
      read,
      state,
      publish(
        response: ChatSettingsMutationResponse,
        onContextChange: ((context: ChatContext) => void) | undefined,
      ) {
        if (get.get(target) !== selected || !observer.hasListeners()) return;
        get.set(local, null);
        client.setQueryData(observer.options.queryKey, {
          kind: "session",
          settings: response.settings,
          session: response.session,
        } satisfies ChatSettingsRead);
        onContextChange?.(response.context);
        if (response.result.kind === "rejected")
          report(t(`chatSettings.rejections.${response.result.reason}`));
      },
      failed(error: Error) {
        if (get.get(target) !== selected || !observer.hasListeners()) return;
        get.set(local, null);
        report(chatOperationFailureMessage(t, error, "settings"));
      },
    };
  });
  const state = Atom.make((get) => get(get(current).state));
  const activate = Atom.fn<
    Readonly<{
      operation: ChatSettingsMutation;
      onContextChange?(context: ChatContext): void;
      completed(result: ChatSettingsMutationResponse | null): void;
      rejected(error: Error): void;
    }>
  >()(
    (input, get) =>
      Effect.gen(function* () {
        const scope = get(current);
        const initial = get(scope.read).data;
        if (initial === undefined || scope.selected === null) {
          input.rejected(new ContractError("Chat Settings are not ready."));
          return;
        }
        get.set(scope.local, {
          source: initial,
          state: settingsReducer(get(scope.state), { kind: "activated", operation: input.operation }),
        });
        if (scope.selected.kind === "new_chat") {
          input.completed(null);
          return;
        }
        const session = scope.selected;
        return yield* Effect.tryPromise(async () => mutation.mutate({ session, input, scope })).pipe(
          Effect.ignore,
        );
      }),
    { concurrent: true },
  );
  type Scope = Atom.Type<typeof current>;
  type Response = Awaited<ReturnType<AppServices["api"]["chat"]["mutateSettings"]>>;
  const mutation = new MutationObserver(client, {
    ...composerRequestOptions,
    mutationFn: async (
      input: Readonly<{
        session: Extract<ChatSettingsTarget, { kind: "session" }>;
        input: Readonly<{
          operation: ChatSettingsMutation;
          onContextChange?(context: ChatContext): void;
          completed(result: Response | null): void;
          rejected(error: Error): void;
        }>;
        scope: Scope;
      }>,
    ) => services.api.chat.mutateSettings(input.session, input.input.operation),
    onSuccess: (result, { scope, input }) => {
      scope.publish(result, input.onContextChange);
      input.completed(result);
    },
    onError: (error: Error, { scope, input }) => {
      scope.failed(error);
      input.rejected(error);
    },
  });
  const requests = queryAtom(mutation);
  const refresh = Atom.fn<undefined>()(
    (_, get) => Effect.promise(async () => get(current).observer.refetch()),
    { concurrent: true },
  );
  return { state, activate, refresh, requests } as const;
}
export type ChatSettingsViewModel = ReturnType<typeof createChatSettingsViewModel>;
