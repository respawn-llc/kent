import { useMutation } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api";
import { useAppServices } from "./useAppServices";
import { useStatusController } from "./useStatusController";
import { openNativeChat } from "./nativeChat";
import type { SessionChatTarget } from "./navigation";

export function useNativeChatOpening() {
  const services = useAppServices();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const mutationFn = useCallback(
    async (target: SessionChatTarget) => openNativeChat(services.nativeBridge, target),
    [services.nativeBridge],
  );
  const mutationOptions = useMemo(
    () => ({ mutationFn, networkMode: "always" as const, retry: false }),
    [mutationFn],
  );
  const { mutate, isPending, variables } = useMutation(mutationOptions);
  const open = useCallback(
    (target: SessionChatTarget) => {
      mutate(target, {
        onError: (error) => {
          push({
            id: `task-chat-pop-out-error-${target.sessionID}`,
            tone: "danger",
            title: t("app.popOutError"),
            body: errorMessage(error),
          });
        },
      });
    },
    [mutate, push, t],
  );
  return {
    open,
    pendingSessionID: isPending ? variables.sessionID : null,
  } as const;
}
