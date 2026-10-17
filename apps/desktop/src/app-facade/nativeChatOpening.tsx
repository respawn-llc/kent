import { MutationObserver, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useAtomSet } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import { createContext, useContext, useMemo, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";

import { errorMessage } from "@/api";
import { useAppServices } from "./useAppServices";
import { openNativeChat } from "./nativeChat";
import type { AppServices } from "./services";
import { queryAction } from "./queryAction";
import { useStatusController } from "./useStatusController";
import type { StatusController } from "./statusContextValue";
import type { SessionChatTarget } from "./navigation";

export function createNativeChatOpening({
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
  const requestsBySession = Atom.family((sessionID: string) =>
    Atom.make((get) => get(get(requests(sessionID)).request).isPending),
  );
  const noRequestPending = Atom.make(false);
  const pending = (sessionID: string | null) =>
    sessionID === null ? noRequestPending : requestsBySession(sessionID);
  const open = Atom.fn<SessionChatTarget>()(
    (target, get) => get.setResult(get(requests(target.sessionID)).submit, target),
    { concurrent: true },
  );
  return { open, pending } as const;
}

type NativeChatOpeningModel = ReturnType<typeof createNativeChatOpening>;

const NativeChatOpeningContext = createContext<NativeChatOpeningModel | null>(null);

export function NativeChatOpeningProvider({ children }: Readonly<{ children: ReactNode }>) {
  const services = useAppServices();
  const client = useQueryClient();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const model = useMemo(
    () => createNativeChatOpening({ client, services, push, t }),
    [client, services, push, t],
  );

  return <NativeChatOpeningContext.Provider value={model}>{children}</NativeChatOpeningContext.Provider>;
}

export function useNativeChatOpening() {
  const model = useContext(NativeChatOpeningContext);
  if (model === null) throw new Error("Native Chat opening provider is missing.");
  return { ...model, open: useAtomSet(model.open) } as const;
}
