import { useMemo } from "react";
import { useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
import { useQueryClient } from "@tanstack/react-query";
import { errorMessage, type ChatSettingsTarget } from "@/api";
import { useAppServices, useOptionalChatRuntimeOwner } from "@/app-facade";
import { showStatusToast } from "@/ui";
import { appI18n } from "@/i18n";
import { createPromptPickerViewModel } from "./PromptPickerViewModel";

export function useChatPromptPicker(target: ChatSettingsTarget | null) {
  const { api, logger } = useAppServices();
  const client = useQueryClient();
  const owner = useOptionalChatRuntimeOwner();
  const presentation = useMemo(() => {
    if (target?.kind !== "session") return Atom.make(null);
    if (owner === null) throw new Error("Session prompt ownership requires ChatRuntimeProvider.");
    const model = createPromptPickerViewModel({
      owner,
      client,
      api: api.chat,
      onError: (error) => {
        void logger.append("warn", "Chat prompt answer batch failed.", {
          error: errorMessage(error),
          sessionID: target.sessionID,
        });
        showStatusToast({
          id: `chat-prompt-send:${target.sessionID}`,
          title: appI18n.t("chat.picker.sendingFailed"),
          body: appI18n.t("chat.picker.sendingFailedBody"),
          tone: "danger",
        });
      },
    });
    return Atom.make((get) => ({
      target,
      state: get(model.state),
      request: get(model.request),
      dispatch: model.dispatch,
      prompts: get(model.prompts),
    }));
  }, [owner, client, api, logger, target]);
  return useAtomValue(presentation);
}
