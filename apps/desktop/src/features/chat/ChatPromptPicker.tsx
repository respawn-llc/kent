import { useMemo, type ReactNode } from "react";
import { useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { errorMessage, type ChatSessionTarget } from "@/api";
import {
  useAppServices,
  useChatRuntimeOwner,
  useChatRuntimeSnapshot,
  usePublishChatPromptPresence,
} from "@/app-facade";
import { showStatusToast } from "@/ui";
import { appI18n } from "@/i18n";
import { createPromptPickerViewModel, usePromptPickerActions } from "./PromptPickerViewModel";
import { PromptPickerView } from "./PromptPickerView";

export function ChatPromptPicker({
  target,
  children,
}: Readonly<{ target: ChatSessionTarget; children: ReactNode }>) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const owner = useChatRuntimeOwner();
  const runtime = useChatRuntimeSnapshot();
  const model = useMemo(
    () =>
      createPromptPickerViewModel({
        owner,
        client,
        api: api.chat,
        onError: (error) => {
          showStatusToast({
            id: `chat-prompt-send:${target.sessionID}`,
            title: appI18n.t("chat.picker.sendingFailed"),
            body: errorMessage(error),
            tone: "danger",
          });
        },
      }),
    [owner, client, api, target],
  );
  const state = useAtomValue(model.state);
  const request = useAtomValue(model.request);
  const actions = usePromptPickerActions(model);
  const visible = state.current !== null;
  usePublishChatPromptPresence(target, visible);
  return (
    <>
      <div hidden={visible} className={visible ? "hidden" : "flex min-h-0 min-w-0 flex-col"}>
        {children}
      </div>
      {visible ? (
        <PromptPickerView
          prompts={runtime.pendingPrompts}
          state={state}
          isPending={request.isPending}
          dispatch={(action, focusField) => {
            actions.dispatch({ action, focusField });
          }}
        />
      ) : null}
    </>
  );
}
