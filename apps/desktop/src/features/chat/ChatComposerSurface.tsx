import { createContext, useContext, useLayoutEffect, type ReactNode } from "react";

import { parsePendingWorkItemID, type ChatRuntimeActivity } from "@/api";
import { useChatRuntimePresentation, useOptionalChatRuntimeSnapshot } from "@/app-facade";
import { Island } from "@/ui";
import { useComposerKeyboard } from "./useComposerKeyboard";
import type { useChatComposer } from "./useChatComposer";
import { useChatPromptPicker } from "./useChatPromptPicker";
import { ChatMessageMotion } from "./ChatMessageMotion";

type Composer = ReturnType<typeof useChatComposer>;
type SurfaceProps = Readonly<{ composer: Composer; children: ReactNode; enabled?: boolean }>;
type SurfaceState = Readonly<{
  composer: Composer;
  activity: ChatRuntimeActivity | null;
  stoppable: boolean;
  onEditorKeyDown: ReturnType<typeof useComposerKeyboard>["onEditorKeyDown"];
  promptPicker: ReturnType<typeof useChatPromptPicker>;
}>;
const ComposerSurfaceContext = createContext<SurfaceState | null>(null);

export function ChatComposerSurface({ composer, children, enabled = true }: SurfaceProps) {
  const promptPicker = useChatPromptPicker(composer.target, composer.interactionRestriction);
  const { activity, observationError } = useChatRuntimePresentation();
  const stoppable = activity?.activeStep !== null && activity?.activeStep !== undefined;
  const keyboard = useComposerKeyboard(composer, stoppable, observationError, {
    enabled,
    ...(activity === null ? {} : { activity }),
  });
  const snapshot = useOptionalChatRuntimeSnapshot();
  const pending = composer.pending.query;
  const observeOutgoing = composer.observeOutgoing;
  useLayoutEffect(() => {
    observeOutgoing({
      committedIDs:
        snapshot?.transcript.items.flatMap((item) =>
          item.kind === "user" ? item.value.QueueItemIDs.map(parsePendingWorkItemID) : [],
        ) ?? [],
      pendingItems: pending.data?.items ?? [],
      pendingFetching: pending.isFetching,
    });
  }, [observeOutgoing, snapshot?.transcript.items, pending.data, pending.isFetching]);
  return (
    <ChatMessageMotion motion={composer.messageMotion}>
      <ComposerSurfaceContext.Provider
        value={{ composer, activity, stoppable, promptPicker, onEditorKeyDown: keyboard.onEditorKeyDown }}
      >
        <div className="h-full min-h-0 min-w-0 p-[var(--space-2)]" {...keyboard.surface}>
          <Island className="h-full min-h-0 w-full min-w-0 overflow-hidden" unpadded>
            {children}
          </Island>
        </div>
      </ComposerSurfaceContext.Provider>
    </ChatMessageMotion>
  );
}

export function useComposerSurface(): SurfaceState {
  const value = useContext(ComposerSurfaceContext);
  if (value === null) throw new Error("ChatComposer requires its ChatComposerSurface.");
  return value;
}
