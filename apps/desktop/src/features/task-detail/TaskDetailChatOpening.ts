import { createContext } from "react";
import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/reactivity/Atom";
import type { TFunction } from "i18next";
import { errorMessage } from "@/api";
import {
  openNativeChat,
  queryAction,
  type AppServices,
  type SessionChatTarget,
  type StatusController,
} from "@/app-facade";

export function createTaskDetailChatOpening({
  client,
  services,
  push,
  t,
}: Readonly<{
  client: QueryClient;
  services: AppServices;
  push: StatusController["push"];
  t: TFunction;
}>) {
  const requests = Atom.family((sessionID: string) =>
    Atom.make(() =>
      queryAction(
        new MutationObserver(client, {
          mutationFn: async (target: SessionChatTarget) => openNativeChat(services.nativeBridge, target),
          retry: false,
          networkMode: "always",
          onError: (error) => {
            push({
              id: `task-chat-pop-out-error-${sessionID}`,
              tone: "danger",
              title: t("app.popOutError"),
              body: errorMessage(error),
            });
          },
        }),
      ),
    ),
  );
  const pending = Atom.family((sessionID: string) =>
    Atom.make((get) => get(get(requests(sessionID)).request).isPending),
  );
  const open = Atom.fn<SessionChatTarget>()(
    (target, get) => get.setResult(get(requests(target.sessionID)).submit, target),
    { concurrent: true },
  );
  return { open, pending } as const;
}

export const TaskDetailChatOpeningContext = createContext<ReturnType<
  typeof createTaskDetailChatOpening
> | null>(null);
