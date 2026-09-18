import type { ReactNode } from "react";
import { usePublishChatPromptPresence } from "@/app-facade";
import { PromptPickerView } from "./PromptPickerView";
import type { useChatPromptPicker } from "./useChatPromptPicker";

export function ChatPromptPicker({
  picker,
  children,
}: Readonly<{
  picker: ReturnType<typeof useChatPromptPicker>;
  children: ReactNode;
}>) {
  const { target, state, request, actions, prompts } = picker;
  const visible = state.current !== null;
  usePublishChatPromptPresence(target, visible);
  return (
    <>
      <div hidden={visible} className={visible ? "hidden" : "flex min-h-0 min-w-0 flex-col"}>
        {children}
      </div>
      {visible ? (
        <PromptPickerView
          prompts={prompts}
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
