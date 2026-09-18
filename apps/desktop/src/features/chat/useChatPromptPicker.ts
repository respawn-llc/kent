import { useMemo } from "react";
import { useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { errorMessage, type ChatSessionTarget } from "@/api";
import { useAppServices, useChatRuntimeOwner, useChatRuntimeSnapshot } from "@/app-facade";
import { showStatusToast } from "@/ui";
import { appI18n } from "@/i18n";
import { createPromptPickerViewModel, usePromptPickerActions } from "./PromptPickerViewModel";

export function useChatPromptPicker(target: ChatSessionTarget) {
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
  return { target, state, request, actions, prompts: runtime.pendingPrompts };
}
