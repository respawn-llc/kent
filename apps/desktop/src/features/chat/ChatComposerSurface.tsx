import { createContext, useContext, type ReactNode } from "react";

import type { ChatRuntimeActivity, ChatSessionTarget } from "@/api";
import { useChatRuntimePresentation } from "@/app-facade";
import { useComposerKeyboard } from "./useComposerKeyboard";
import type { useChatComposer } from "./useChatComposer";
import { useChatPromptPicker } from "./useChatPromptPicker";

type Composer = ReturnType<typeof useChatComposer>;
type SurfaceProps = Readonly<{ composer: Composer; children: ReactNode }>;
type SurfaceState = Readonly<{
  composer: Composer;
  activity: ChatRuntimeActivity | null;
  stoppable: boolean;
  onEditorKeyDown: ReturnType<typeof useComposerKeyboard>["onEditorKeyDown"];
  promptPicker: ReturnType<typeof useChatPromptPicker> | null;
}>;
const ComposerSurfaceContext = createContext<SurfaceState | null>(null);

export function ChatComposerSurface({ composer, children }: SurfaceProps) {
  return composer.target.kind === "session" ? (
    <SessionComposerSurface composer={composer} target={composer.target}>
      {children}
    </SessionComposerSurface>
  ) : (
    <ComposerSurface composer={composer} promptPicker={null}>
      {children}
    </ComposerSurface>
  );
}

function SessionComposerSurface({
  target,
  ...props
}: SurfaceProps & Readonly<{ target: ChatSessionTarget }>) {
  const promptPicker = useChatPromptPicker(target);
  return <ComposerSurface {...props} promptPicker={promptPicker} />;
}

function ComposerSurface({
  composer,
  children,
  promptPicker,
}: SurfaceProps & Pick<SurfaceState, "promptPicker">) {
  const { activity, observationError } = useChatRuntimePresentation();
  const stoppable = activity?.activeStep !== null && activity?.activeStep !== undefined;
  const keyboard = useComposerKeyboard(composer, stoppable, observationError);
  return (
    <ComposerSurfaceContext.Provider
      value={{ composer, activity, stoppable, promptPicker, onEditorKeyDown: keyboard.onEditorKeyDown }}
    >
      <div className="h-full min-h-0" {...keyboard.surface}>
        {children}
      </div>
    </ComposerSurfaceContext.Provider>
  );
}

export function useComposerSurface(): SurfaceState {
  const value = useContext(ComposerSurfaceContext);
  if (value === null) throw new Error("ChatComposer requires its ChatComposerSurface.");
  return value;
}
