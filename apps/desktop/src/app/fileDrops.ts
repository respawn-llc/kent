import { useEffect, useMemo } from "react";
import { useAtomSuspense } from "@effect/atom-react";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import * as Atom from "effect/reactivity/Atom";

import type { AppServices } from "@/app-facade";
import { insertTextAtSelection } from "@/shared/text-input";

export function useWindowFileDrops({ nativeBridge, logger }: AppServices): void {
  const drops = useMemo(
    () =>
      Atom.make(
        nativeBridge.window
          .fileDrops(async () => logger.reportObservationOverflow("file-drop"))
          .pipe(
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
  if (!(input instanceof HTMLTextAreaElement || input instanceof HTMLInputElement) || paths.length === 0) {
    return;
  }
  insertTextAtSelection(input, paths.join(" "));
}
