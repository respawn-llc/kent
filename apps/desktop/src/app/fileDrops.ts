import { useEffect, useMemo } from "react";
import { useAtomSuspense } from "@effect/atom-react";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import * as Atom from "effect/unstable/reactivity/Atom";

import { shellObservationDiagnostics, type AppServices } from "@/app-facade";

export function useWindowFileDrops({ nativeBridge, logger }: AppServices): void {
  const drops = useMemo(
    () =>
      Atom.make(
        nativeBridge.window.fileDrops(shellObservationDiagnostics(logger, "file-drop")).pipe(
          Stream.runForEach((paths) =>
            Effect.sync(() => {
              insertFilePaths(paths);
            }),
          ),
          Effect.catch((error) =>
            Effect.promise(async () =>
              logger.append("error", "Could not listen for native file drops", { error: String(error) }),
            ),
          ),
          Effect.as(null),
        ),
        { initialValue: null },
      ),
    [nativeBridge, logger],
  );
  useAtomSuspense(drops);
  useEffect(() => {
    const preventFileNavigation = (event: DragEvent) => {
      if (event.dataTransfer?.types.includes("Files")) {
        event.preventDefault();
        event.stopPropagation();
      }
    };
    document.addEventListener("dragover", preventFileNavigation, true);
    document.addEventListener("drop", preventFileNavigation, true);
    return () => {
      document.removeEventListener("dragover", preventFileNavigation, true);
      document.removeEventListener("drop", preventFileNavigation, true);
    };
  }, []);
}

function insertFilePaths(paths: readonly string[]): void {
  const input = document.activeElement;
  if (
    !(input instanceof HTMLTextAreaElement || input instanceof HTMLInputElement) ||
    input.disabled ||
    input.readOnly ||
    input.selectionStart === null ||
    input.selectionEnd === null ||
    paths.length === 0
  ) {
    return;
  }
  const text = paths.join(" ");
  const start = input.selectionStart;
  input.setRangeText(text, start, input.selectionEnd, "end");
  input.dispatchEvent(new Event("input", { bubbles: true }));
}
