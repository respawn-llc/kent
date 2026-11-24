import { useEffect, useMemo } from "react";
import { useAtomSuspense } from "@effect/atom-react";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import * as Atom from "effect/reactivity/Atom";

import type { AppServices } from "@/app-facade";
import { insertTextAtSelection, isEditableTextInput } from "@/shared/text-input";

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
    const handleDragEvent = (event: DragEvent) => {
      if (event.dataTransfer?.types.includes("Files")) {
        event.preventDefault();
        event.stopPropagation();
        return;
      }
      const transfer = event.dataTransfer;
      if (transfer === null) return;
      if (!transfer.types.includes("text/uri-list") && !transfer.types.includes("text/plain")) return;
      if (event.type === "dragover") {
        if (dropTarget() !== null) event.preventDefault();
        return;
      }
      const uriList = transfer.getData("text/uri-list");
      const text = uriList || transfer.getData("text/plain");
      const lines = text
        .split("\n")
        .map((line) => line.trim())
        .filter((line) => line.length > 0);
      if (!uriList && !lines.every((line) => URL.canParse(line))) return;
      const urls = lines.filter((line) => URL.canParse(line));
      if (urls.length > 0) {
        event.preventDefault();
        event.stopPropagation();
        insertDroppedText(urls.join(" "));
      }
    };
    document.addEventListener("dragover", handleDragEvent, true);
    document.addEventListener("drop", handleDragEvent, true);
    return () => {
      document.removeEventListener("dragover", handleDragEvent, true);
      document.removeEventListener("drop", handleDragEvent, true);
    };
  }, []);
}

function insertFilePaths(paths: readonly string[]): void {
  if (paths.length > 0) insertDroppedText(paths.join(" "));
}

function insertDroppedText(text: string): void {
  const input = dropTarget();
  if (input !== null) insertTextAtSelection(input, text);
}

function dropTarget(): HTMLTextAreaElement | HTMLInputElement | null {
  const focused = document.activeElement;
  if (isEditableTextInput(focused)) return focused;
  let target: HTMLTextAreaElement | HTMLInputElement | null = null;
  for (const input of document.querySelectorAll("textarea, input")) {
    if (!isEditableTextInput(input) || !isVisible(input)) continue;
    if (target !== null) return null;
    target = input;
  }
  return target;
}

function isVisible(input: HTMLElement): boolean {
  if (input.closest("[hidden], [inert], [aria-hidden='true']") !== null) return false;
  const style = getComputedStyle(input);
  if (style.visibility !== "visible") return false;
  const bounds = input.getBoundingClientRect();
  return (
    bounds.width > 0 &&
    bounds.height > 0 &&
    bounds.bottom > 0 &&
    bounds.right > 0 &&
    bounds.top < window.innerHeight &&
    bounds.left < window.innerWidth
  );
}
