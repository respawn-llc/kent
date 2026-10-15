import { useAtomSet } from "@effect/atom-react";
import { usePublishChatPromptPresence } from "@/app-facade";
import { PromptPickerView } from "./PromptPickerView";
import type { useChatPromptPicker } from "./useChatPromptPicker";
import { useComposerSurface } from "./ChatComposerSurface";

export function ChatPromptPicker({
  picker,
}: Readonly<{
  picker: NonNullable<ReturnType<typeof useChatPromptPicker>>;
}>) {
  const { target, state, request, prompts } = picker;
  const dispatch = useAtomSet(picker.dispatch);
  const { speech, promptEditor } = useComposerSurface();
  const visible = state.current !== null;
  usePublishChatPromptPresence(target, visible);
  return visible ? (
    <PromptPickerView
      prompts={prompts}
      state={state}
      isPending={request.isPending}
      editorRef={promptEditor}
      dispatch={(action, focusField) => {
        if (speech.busy && action.kind === "confirm") return;
        dispatch({
          action: speech.busy && action.kind === "activate" ? { ...action, kind: "select" } : action,
          focusField,
        });
      }}
    />
  ) : null;
}
