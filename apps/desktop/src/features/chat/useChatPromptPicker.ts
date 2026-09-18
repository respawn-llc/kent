import { useMemo } from "react";
import { useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
import { useQueryClient } from "@tanstack/react-query";
import { errorMessage, type ChatSettingsTarget } from "@/api";
import { useAppServices, useOptionalChatRuntimeOwner } from "@/app-facade";
import { showStatusToast } from "@/ui";
import { appI18n } from "@/i18n";
import { createPromptPickerViewModel } from "./PromptPickerViewModel";

export function useChatPromptPicker(target: ChatSettingsTarget) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const owner = useOptionalChatRuntimeOwner();
  const presentation = useMemo(() => {
    if (target.kind === "new_chat") return Atom.make(null);
    if (owner === null) throw new Error("Session prompt ownership requires ChatRuntimeProvider.");
    const model = createPromptPickerViewModel({
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
    });
    return Atom.make((get) => ({
      target,
      state: get(model.state),
      request: get(model.request),
      dispatch: model.dispatch,
      prompts: get(model.prompts),
    }));
  }, [owner, client, api, target]);
  return useAtomValue(presentation);
}
