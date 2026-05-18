export function startProjectToBoardTransition(_source: HTMLElement, update: () => void | Promise<void>): void {
  const reducedMotion =
    typeof globalThis.matchMedia === "function" &&
    globalThis.matchMedia("(prefers-reduced-motion: reduce)").matches;
  if (
    typeof document.startViewTransition !== "function" ||
    reducedMotion
  ) {
    void update();
    return;
  }

  document.startViewTransition(update);
}
