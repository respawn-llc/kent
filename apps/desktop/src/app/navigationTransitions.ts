export const projectToBoardTransitionName = "builder-project-to-board";

export function startProjectToBoardTransition(source: HTMLElement, update: () => void | Promise<void>): void {
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

  const previousName = source.style.viewTransitionName;
  source.style.viewTransitionName = projectToBoardTransitionName;
  document.documentElement.dataset.builderNavigationTransition = "project-to-board";
  const transition = document.startViewTransition(update);
  const restoreSource = () => {
    source.style.viewTransitionName = previousName;
    delete document.documentElement.dataset.builderNavigationTransition;
  };
  void transition.finished.then(restoreSource, restoreSource);
}
