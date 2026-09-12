import { useEffect } from "react";

import type { AppServices } from "@/app-facade";

export function useWindowFileDrops({ nativeBridge, logger }: AppServices): void {
  useEffect(() => {
    const preventFileNavigation = (event: DragEvent) => {
      if (event.dataTransfer?.types.includes("Files")) {
        event.preventDefault();
        event.stopPropagation();
      }
    };
    document.addEventListener("dragover", preventFileNavigation, true);
    document.addEventListener("drop", preventFileNavigation, true);
    let disposed = false;
    let unlisten: (() => void) | undefined;
    void nativeBridge.window
      .onFileDrop((paths) => {
        if (!disposed) insertFilePaths(paths);
      })
      .then((stop) => {
        if (disposed) stop();
        else unlisten = stop;
      })
      .catch((error: unknown) => {
        void logger.append("error", "Could not listen for native file drops", { error: String(error) });
      });
    return () => {
      disposed = true;
      unlisten?.();
      document.removeEventListener("dragover", preventFileNavigation, true);
      document.removeEventListener("drop", preventFileNavigation, true);
    };
  }, [nativeBridge, logger]);
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
