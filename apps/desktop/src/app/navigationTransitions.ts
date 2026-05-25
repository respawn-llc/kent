export async function runNavigationTransition(update: () => void | Promise<void>): Promise<void> {
  const reducedMotion =
    typeof globalThis.matchMedia === "function" &&
    globalThis.matchMedia("(prefers-reduced-motion: reduce)").matches;
  if (
    typeof document.startViewTransition !== "function" ||
    reducedMotion
  ) {
    await update();
    return;
  }

  const transition = document.startViewTransition(update);
  await transition.updateCallbackDone;
}
