import { useAtomSet } from "@effect/atom-react";
import { useAppServices, usePublishChatPromptPresence } from "@/app-facade";
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
  const { nativeBridge } = useAppServices();
  const visible = state.current !== null;
  usePublishChatPromptPresence(target, visible);
  return visible ? (
    <div
      className="contents"
      onKeyDownCapture={(event) => {
        if (
          speech.phase !== "recording" ||
          event.key !== "Enter" ||
          event.shiftKey ||
          event.altKey ||
          event.nativeEvent.isComposing
        )
          return;
        event.preventDefault();
        event.stopPropagation();
        const submit = nativeBridge.capabilities.platform === "macos" ? event.metaKey : event.ctrlKey;
        void speech.finish().then((completed) => {
          if (completed && submit) dispatch({ action: { kind: "confirm" } });
        });
      }}
    >
      <PromptPickerView
        prompts={prompts}
        state={state}
        isPending={request.isPending}
        editorRef={promptEditor}
        dispatch={(action, focusField) => {
          if (speech.busy && (action.kind === "confirm" || action.kind === "decline")) return;
          dispatch({
            action: speech.busy && action.kind === "activate" ? { ...action, kind: "select" } : action,
            focusField,
          });
        }}
      />
    </div>
  ) : null;
}
