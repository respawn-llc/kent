import { createContext, useContext, type ReactNode } from "react";

import type { ChatRuntimeActivity } from "@/api";
import { useChatRuntimeActivity, useChatRuntimeSnapshot } from "@/app-facade";
import { useComposerKeyboard } from "./useComposerKeyboard";
import type { useChatComposer } from "./useChatComposer";

type Composer = ReturnType<typeof useChatComposer>;
type SurfaceProps = Readonly<{ composer: Composer; children: ReactNode }>;
type SurfaceState = Readonly<{
  composer: Composer;
  activity: ChatRuntimeActivity | null;
  stoppable: boolean;
  onEditorKeyDown: ReturnType<typeof useComposerKeyboard>["onEditorKeyDown"];
}>;
const ComposerSurfaceContext = createContext<SurfaceState | null>(null);

export function ChatComposerSurface(props: SurfaceProps) {
  return props.composer.target.kind === "session" ? (
    <SessionComposerSurface {...props} />
  ) : (
    <ComposerSurface {...props} activity={null} observationError={null} />
  );
}

function SessionComposerSurface(props: SurfaceProps) {
  const activity = useChatRuntimeActivity();
  const { observation } = useChatRuntimeSnapshot();
  return (
    <ComposerSurface
      {...props}
      activity={activity}
      observationError={observation.kind === "error" ? observation.error : null}
    />
  );
}

function ComposerSurface({
  composer,
  children,
  activity,
  observationError,
}: SurfaceProps & Readonly<{ activity: ChatRuntimeActivity | null; observationError: Error | null }>) {
  const stoppable = activity?.activeStep !== null && activity?.activeStep !== undefined;
  const keyboard = useComposerKeyboard(composer, stoppable, observationError);
  return (
    <ComposerSurfaceContext.Provider
      value={{ composer, activity, stoppable, onEditorKeyDown: keyboard.onEditorKeyDown }}
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
