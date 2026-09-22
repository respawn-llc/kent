import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";

import { useAppServices, useWindowFocus } from "@/app-facade";
import { showStatusToast, motionDurationFromCSSVar, prefersReducedMotion } from "@/ui";
import { advanceComposerStop, type ComposerStopEvent } from "./composerStop";
import type { useChatComposer } from "./useChatComposer";

export function useComposerKeyboard(
  composer: ReturnType<typeof useChatComposer>,
  stoppable: boolean,
  observationError: Error | null,
  enabled = true,
) {
  const { nativeBridge } = useAppServices();
  const { t } = useTranslation();
  const surface = useRef<HTMLDivElement>(null);
  const [placement, setPlacement] = useState<Readonly<{
    editor: HTMLTextAreaElement;
    cursor: "start" | "end";
  }> | null>(null);
  useLayoutEffect(() => {
    if (placement === null) return;
    const offset = placement.cursor === "start" ? 0 : placement.editor.value.length;
    placement.editor.setSelectionRange(offset, offset);
  }, [placement]);
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
      ref: surface,
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
        if (!enabled) return;
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
      if (!enabled) return;
      if (handled(event) || handlePickerKey(composer, event)) return;
      const direction = historyDirection(event);
      if (direction !== null) {
        const editor = event.currentTarget;
        event.preventDefault();
        void composer.navigateHistory(direction).then((movement) => {
          if (movement.kind === "replaced" || movement.kind === "restored") {
            setPlacement({ editor, cursor: movement.cursor });
          } else if (movement.kind === "blocked") {
            showStatusToast({
              id: "chat-composer-history-blocked",
              tone: "warning",
              title: t("chatComposer.historyBlocked"),
            });
            if (!prefersReducedMotion())
              surface.current?.animate(
                [
                  { transform: "translateX(0)" },
                  { transform: "translateX(calc(-1 * var(--space-1)))" },
                  { transform: "translateX(var(--space-1))" },
                  { transform: "translateX(calc(-1 * var(--space-1)))" },
                  { transform: "translateX(0)" },
                ],
                { duration: motionDurationFromCSSVar("--motion-morph", 180), easing: "ease-in-out" },
              );
          }
        });
        return;
      }
      if (event.key === "Enter" && !event.shiftKey && !event.altKey && !event.metaKey) {
        event.preventDefault();
        composer.submit(event.ctrlKey ? "queue" : "send");
      }
    },
  };
}

function historyDirection(event: KeyboardEvent<HTMLTextAreaElement>): -1 | 1 | null {
  if (event.shiftKey || event.altKey || event.metaKey || event.ctrlKey) return null;
  if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return null;
  const direction = event.key === "ArrowUp" ? -1 : 1;
  const editor = event.currentTarget;
  const boundary = direction === -1 ? 0 : editor.value.length;
  return editor.selectionStart === editor.selectionEnd && editor.selectionStart === boundary
    ? direction
    : null;
}

function handled(event: KeyboardEvent): boolean {
  return event.defaultPrevented || event.nativeEvent.isComposing;
}

function escapeStopPlatform(platform: string): boolean {
  return platform === "windows" || platform === "linux";
}

function handlePickerKey(composer: ReturnType<typeof useChatComposer>, event: KeyboardEvent): boolean {
  if (!composer.pickerOpen) return false;
  if (event.key === "Escape") {
    composer.dismissPicker();
    event.preventDefault();
    event.stopPropagation();
    return true;
  }
  if (composer.suggestions.length > 0 && (event.key === "ArrowUp" || event.key === "ArrowDown")) {
    composer.moveCommand(event.key === "ArrowUp" ? -1 : 1);
    event.preventDefault();
    return true;
  }
  return false;
}
