import { useAtomSet } from "@effect/atom-react";
import { usePublishChatPromptPresence } from "@/app-facade";
import { PromptPickerView } from "./PromptPickerView";
import type { useChatPromptPicker } from "./useChatPromptPicker";

export function ChatPromptPicker({
  picker,
}: Readonly<{
  picker: NonNullable<ReturnType<typeof useChatPromptPicker>>;
}>) {
  const { target, state, request, prompts } = picker;
  const dispatch = useAtomSet(picker.dispatch);
  const visible = state.current !== null;
  usePublishChatPromptPresence(target, visible);
  return visible ? (
    <PromptPickerView
      prompts={prompts}
      state={state}
      isPending={request.isPending}
      dispatch={(action, focusField) => {
        dispatch({ action, focusField });
      }}
    />
  ) : null;
}
