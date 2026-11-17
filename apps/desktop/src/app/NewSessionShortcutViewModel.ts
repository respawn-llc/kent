import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";
import type { TFunction } from "i18next";
import { errorMessage } from "@/api";
import {
  openNativeNewChat,
  queryAtom,
  queryKeys,
  type AppServices,
  type StatusController,
} from "@/app-facade";

export type NewSessionSource =
  | Readonly<{ kind: "project"; projectID: string }>
  | Readonly<{ kind: "task"; taskID: string }>
  | Readonly<{ kind: "session"; projectID: string; sessionID: string }>;

type Input = Readonly<{
  source: NewSessionSource;
  newWindow: boolean;
  openNewChat(projectID: string): Promise<void>;
}>;

export function createNewSessionShortcutViewModel({
  services,
  client,
  t,
  push,
}: Readonly<{ services: AppServices; client: QueryClient; t: TFunction; push: StatusController["push"] }>) {
  const observer = new MutationObserver(client, {
    retry: false,
    networkMode: "always",
    mutationFn: async ({ source, newWindow, openNewChat }: Input) => {
      const projectID =
        source.kind === "task"
          ? (
              await client.fetchQuery({
                queryKey: queryKeys.task(source.taskID),
                queryFn: async () => services.api.getTask(source.taskID),
              })
            ).projectID
          : source.projectID;
      if (source.kind === "session") {
        const settings = await services.api.chat.getSettings({
          kind: "session",
          projectID,
          sessionID: source.sessionID,
        });
        if (settings.kind !== "session") throw new Error("Expected Session settings");
        await openNativeNewChat(
          services.nativeBridge,
          projectID,
          t("chat.newChat"),
          settings.settings.selectedAgent.role,
        );
      } else if (newWindow) {
        await openNativeNewChat(services.nativeBridge, projectID, t("chat.newChat"));
      } else {
        await openNewChat(projectID);
      }
    },
    onError: (error) => {
      push({
        id: "new-session-shortcut-error",
        tone: "danger",
        title: t("states.error"),
        body: errorMessage(error),
      });
    },
  });
  const request = queryAtom(observer);
  const activate = Atom.fn<Input>()(
    (input) =>
      Effect.gen(function* () {
        if (observer.getCurrentResult().isPending) return;
        yield* Effect.tryPromise(async () => observer.mutate(input)).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  return { request, activate };
}
