export type SidebarResizeBounds = Readonly<{
  maxWidthPx: number;
  shellWidthPx: number;
}>;

export const sidebarInitialMaxWidthPx = 840;
export const sidebarInitialWidthRatio = 0.35;
export const sidebarMaxWidthRatio = 0.85;
export const sidebarMinWidthPx = 360;
export const sidebarResizeStepPx = 32;

export function clampSidebarWidth(widthPx: number, maxWidthPx = Number.MAX_SAFE_INTEGER): number {
  return Math.min(Math.max(Math.round(widthPx), sidebarMinWidthPx), maxWidthPx);
}

export function initialSidebarWidthForViewport(viewportWidthPx: number): number {
  return clampSidebarWidth(viewportWidthPx * sidebarInitialWidthRatio, sidebarInitialMaxWidthPx);
}

export function sidebarResizeBoundsForShellWidth(shellWidthPx: number): SidebarResizeBounds {
  const roundedShellWidthPx = Math.max(0, Math.round(shellWidthPx));
  return {
    maxWidthPx: Math.max(sidebarMinWidthPx, Math.round(roundedShellWidthPx * sidebarMaxWidthRatio)),
    shellWidthPx: roundedShellWidthPx,
  };
}
