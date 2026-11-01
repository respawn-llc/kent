import { createContext, useContext, useLayoutEffect, useRef, type ReactNode, type RefObject } from "react";

import { pendingWorkItemIDFromProto, type ChatRuntimeActivity } from "@/api";
import { useChatRuntimePresentation, useOptionalChatRuntimeSnapshot } from "@/app-facade";
import { Island } from "@/ui";
import { useComposerKeyboard } from "./useComposerKeyboard";
import type { useChatComposer } from "./useChatComposer";
import { useChatPromptPicker } from "./useChatPromptPicker";
import { useSpeechField } from "@/shared/speech-input";
import { ChatMessageMotion } from "./ChatMessageMotion";

type Composer = ReturnType<typeof useChatComposer>;
type SurfaceProps = Readonly<{ composer: Composer; children: ReactNode; enabled?: boolean }>;
type SurfaceState = Readonly<{
  composer: Composer;
  activity: ChatRuntimeActivity | null;
  stoppable: boolean;
  onEditorKeyDown: ReturnType<typeof useComposerKeyboard>["onEditorKeyDown"];
  onSendClick: ReturnType<typeof useComposerKeyboard>["onSendClick"];
  queueHeld: boolean;
  promptPicker: ReturnType<typeof useChatPromptPicker>;
  speech: ReturnType<typeof useSpeechField>;
  promptEditor: RefObject<HTMLTextAreaElement | null>;
}>;
const ComposerSurfaceContext = createContext<SurfaceState | null>(null);

export function ChatComposerSurface({ composer: original, children, enabled = true }: SurfaceProps) {
  const promptPicker = useChatPromptPicker(original.target, original.interactionRestriction);
  const editor = useRef<HTMLTextAreaElement>(null);
  const promptEditor = useRef<HTMLTextAreaElement>(null);
  const owner = useRef({ original, promptPicker });
  useLayoutEffect(() => {
    owner.current = { original, promptPicker };
  });
  const speechField = useSpeechField({
    value: original.text,
    onChange: original.edit,
    editorRef: editor,
    readOnly:
      original.interactionRestricted ||
      original.draft.kind === "loading" ||
      promptCommentaryReadOnly(promptPicker),
    captureDraft: () => {
      const visibleDraft = () => {
        const current = owner.current;
        const promptID = current.promptPicker?.state.current;
        if (promptID == null)
          return {
            read: () => current.original.text,
            write: (text: string) => {
              current.original.edit(text);
            },
            editor: () => editor.current,
          };
        return {
          read: () => {
            const draft = current.promptPicker?.state.drafts.get(promptID);
            return draft === undefined || draft.status === "declined" ? null : draft.commentary;
          },
          write: (text: string) => {
            current.promptPicker?.editCommentary({ toolCallID: promptID, text });
          },
          editor: () => promptEditor.current,
        };
      };
      return {
        read: () => visibleDraft().read(),
        write: (text) => {
          visibleDraft().write(text);
        },
        editor: () => visibleDraft().editor(),
      };
    },
  });
  const speech = {
    ...speechField,
    finish: async () => {
      const promptID = owner.current.promptPicker?.state.current ?? null;
      const completed = await speechField.finish();
      return completed && promptID === (owner.current.promptPicker?.state.current ?? null);
    },
  };
  const composer = {
    ...original,
    canSubmit:
      speechField.phase === "recording" ? original.submissionReady : original.canSubmit && !speechField.busy,
    submit: (...args: Parameters<Composer["submit"]>) => {
      if (!speech.busy) {
        original.submit(...args);
      } else if (speech.phase === "recording" && original.submissionReady) {
        void speech.finish().then((completed) => {
          if (completed && owner.current.original.canSubmit) owner.current.original.submit(...args);
        });
      }
    },
  };
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
          item.kind === "user" ? item.value.QueueItemIDs.map(pendingWorkItemIDFromProto) : [],
        ) ?? [],
      pendingItems: pending.data?.items ?? [],
      pendingFetching: pending.isFetching,
    });
  }, [observeOutgoing, snapshot?.transcript.items, pending.data, pending.isFetching]);
  return (
    <ChatMessageMotion motion={composer.messageMotion}>
      <ComposerSurfaceContext.Provider
        value={{
          composer,
          activity,
          stoppable,
          promptPicker,
          speech,
          promptEditor,
          onEditorKeyDown: keyboard.onEditorKeyDown,
          onSendClick: keyboard.onSendClick,
          queueHeld: keyboard.queueHeld,
        }}
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

function promptCommentaryReadOnly(picker: ReturnType<typeof useChatPromptPicker>): boolean {
  if (picker?.state.current == null) return false;
  return picker.request.isPending || picker.state.drafts.get(picker.state.current)?.status === "declined";
}

export function useComposerSurface(): SurfaceState {
  const value = useContext(ComposerSurfaceContext);
  if (value === null) throw new Error("ChatComposer requires its ChatComposerSurface.");
  return value;
}
