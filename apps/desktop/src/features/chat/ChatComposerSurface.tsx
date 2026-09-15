import { createContext, useContext, type ReactNode } from "react";

import type { ChatRuntimeActivity } from "@/api";
import { useChatRuntimeActivity, useConnectionSnapshot } from "@/app-facade";
import { useComposerKeyboard } from "./useComposerKeyboard";
import type { useChatComposer } from "./useChatComposer";

type Composer = ReturnType<typeof useChatComposer>;
type SurfaceProps = Readonly<{ composer: Composer; children: ReactNode }>;
type SurfaceState = Readonly<{
  composer: Composer;
  activity: ChatRuntimeActivity | null;
  connected: boolean;
  stoppable: boolean;
  onEditorKeyDown: ReturnType<typeof useComposerKeyboard>["onEditorKeyDown"];
}>;
const ComposerSurfaceContext = createContext<SurfaceState | null>(null);

export function ChatComposerSurface(props: SurfaceProps) {
  return props.composer.target.kind === "session" ? (
    <SessionComposerSurface {...props} />
  ) : (
    <ComposerSurface {...props} activity={null} />
  );
}

function SessionComposerSurface(props: SurfaceProps) {
  const activity = useChatRuntimeActivity();
  return <ComposerSurface {...props} activity={activity} />;
}

function ComposerSurface({
  composer,
  children,
  activity,
}: SurfaceProps & Readonly<{ activity: ChatRuntimeActivity | null }>) {
  const connection = useConnectionSnapshot();
  const connected = connection.phase === "connected";
  const stoppable = activity?.activeStep !== null && activity?.activeStep !== undefined;
  const keyboard = useComposerKeyboard(composer, connected, stoppable);
  return (
    <ComposerSurfaceContext.Provider
      value={{ composer, activity, connected, stoppable, onEditorKeyDown: keyboard.onEditorKeyDown }}
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
