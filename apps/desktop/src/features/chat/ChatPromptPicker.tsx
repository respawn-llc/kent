import { useCallback, useEffect, useMemo, useSyncExternalStore, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { errorMessage, type ChatSessionTarget } from "@/api";
import {
  useAppServices,
  useChatRuntimeOwner,
  useChatRuntimeSnapshot,
  useConnectionSnapshot,
  usePublishChatPromptPresence,
} from "@/app-facade";
import { showStatusToast } from "@/ui";
import { PromptPickerController } from "./PromptPickerController";
import { PromptPickerView } from "./PromptPickerView";

export function ChatPromptPicker({
  target,
  children,
}: Readonly<{ target: ChatSessionTarget; children: ReactNode }>) {
  const { api } = useAppServices();
  const { t } = useTranslation();
  const owner = useChatRuntimeOwner();
  const runtime = useChatRuntimeSnapshot();
  const connection = useConnectionSnapshot();
  const onError = useCallback(
    (error: unknown) => {
      showStatusToast({
        id: `chat-prompt-send:${target.sessionID}`,
        title: t("chat.picker.sendingFailed"),
        body: errorMessage(error),
        tone: "danger",
      });
    },
    [t, target.sessionID],
  );
  const controller = useMemo(
    () => new PromptPickerController(owner, api.chat, target, { onError, connection: api.connection }),
    [owner, api, target, onError],
  );
  useEffect(() => controller.mount(), [controller]);
  const snapshot = useSyncExternalStore(
    controller.subscribe,
    () => controller.snapshot,
    () => controller.snapshot,
  );
  const visible = snapshot.state.current !== null;
  usePublishChatPromptPresence(target, visible);
  return (
    <>
      <div hidden={visible} className="min-h-0 min-w-0">
        {children}
      </div>
      {visible ? (
        <PromptPickerView
          prompts={runtime.pendingPrompts}
          state={snapshot.state}
          isPending={snapshot.isPending}
          disconnected={connection.phase !== "connected"}
          dispatch={(action) => controller.dispatch(action)}
        />
      ) : null}
    </>
  );
}
