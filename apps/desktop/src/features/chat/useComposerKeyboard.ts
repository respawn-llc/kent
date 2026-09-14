import { useEffect, useRef, type KeyboardEvent } from "react";

import { useAppServices, useWindowFocus } from "@/app-facade";
import { advanceComposerStop, type ComposerStopEvent } from "./composerStop";
import type { useChatComposer } from "./useChatComposer";

export function useComposerKeyboard(
  composer: ReturnType<typeof useChatComposer>,
  stoppable: boolean,
  observationError: Error | null,
) {
  const { nativeBridge } = useAppServices();
  const platform = nativeBridge.capabilities.platform;
  const stopAvailable = stoppable;
  const focused = useWindowFocus();
  const deadline = useRef<number | null>(null);
  useEffect(() => {
    if (observationError !== null) deadline.current = null;
  }, [observationError]);
  function advance(event: ComposerStopEvent) {
    const result = advanceComposerStop(deadline.current, event);
    deadline.current = result.deadline;
    return result.stop;
  }
  useEffect(() => {
    if (!stoppable) deadline.current = null;
  }, [stoppable]);
  useEffect(() => {
    deadline.current = null;
  }, [focused]);
  useEffect(
    () => () => {
      deadline.current = null;
    },
    [],
  );
  return {
    surface: {
      onKeyDownCapture: (event: KeyboardEvent) => {
        if (event.key !== "Escape") advance({ kind: "keyboard" });
      },
      onPointerDownCapture: () => {
        advance({ kind: "pointer" });
      },
      onFocusCapture: () => {
        advance({ kind: "focus" });
      },
      onBlurCapture: () => {
        advance({ kind: "focus" });
      },
      onKeyDown: (event: KeyboardEvent) => {
        if (handled(event)) return;
        if (event.key === "Escape" && handlePickerKey(composer, event)) return;
        if (platform === "macos" && event.metaKey && event.key === "." && stopAvailable) {
          event.preventDefault();
          composer.pending.stop();
        } else if (escapeStopPlatform(platform) && event.key === "Escape") {
          if (
            advance({
              kind: "escape",
              now: performance.now(),
              handled: event.defaultPrevented,
              stoppable: stopAvailable,
            })
          ) {
            event.preventDefault();
            composer.pending.stop();
          }
        }
      },
    },
    onEditorKeyDown: (event: KeyboardEvent<HTMLTextAreaElement>) => {
      if (handled(event) || handlePickerKey(composer, event)) return;
      if (event.key === "Enter" && !event.shiftKey && !event.altKey && !event.metaKey) {
        event.preventDefault();
        composer.submit(event.ctrlKey ? "queue" : "send");
      }
    },
  };
}

function handled(event: KeyboardEvent): boolean {
  return event.defaultPrevented || event.nativeEvent.isComposing;
}

function escapeStopPlatform(platform: string): boolean {
  return platform === "windows" || platform === "linux";
}

function handlePickerKey(composer: ReturnType<typeof useChatComposer>, event: KeyboardEvent): boolean {
  if (composer.suggestions.length === 0) return false;
  if (event.key === "Escape") {
    composer.dismissPicker();
    event.preventDefault();
    event.stopPropagation();
    return true;
  }
  if (event.key === "ArrowUp" || event.key === "ArrowDown") {
    composer.moveCommand(event.key === "ArrowUp" ? -1 : 1);
    event.preventDefault();
    return true;
  }
  return false;
}
