export type ComposerStopEvent =
  | Readonly<{ kind: "escape"; now: number; handled: boolean; stoppable: boolean }>
  | Readonly<{
      kind: "keyboard" | "pointer" | "focus" | "disposal" | "disconnected" | "completed" | "timeout";
    }>;

export function advanceComposerStop(
  deadline: number | null,
  event: ComposerStopEvent,
): Readonly<{ deadline: number | null; stop: boolean }> {
  if (event.kind !== "escape") return { deadline: null, stop: false };
  if (!event.stoppable) return { deadline: null, stop: false };
  if (event.handled) return { deadline, stop: false };
  if (deadline !== null && event.now <= deadline) return { deadline: null, stop: true };
  return { deadline: event.now + 2000, stop: false };
}
