export type SidebarResizeBounds = Readonly<{
  maxWidthPx: number;
  minWidthPx: number;
  shellWidthPx: number;
}>;

export type ResolvedSidebarWidth = Readonly<{
  px: number;
}>;

export const sidebarMaxWidthRatio = 0.85;
export const sidebarMinWidthPx = 350;
export const sidebarResizeStepPx = 32;

export type SidebarSizePreference = Readonly<{
  desiredWidthPx: number;
  minWidthPx: number;
}>;

export const defaultSidebarSizePreference: SidebarSizePreference = {
  desiredWidthPx: 550,
  minWidthPx: sidebarMinWidthPx,
};

export function clampSidebarWidth(
  widthPx: number,
  maxWidthPx = Number.MAX_SAFE_INTEGER,
  minWidthPx = sidebarMinWidthPx,
): number {
  const roundedMaxWidthPx = Math.max(0, Math.round(maxWidthPx));
  const effectiveMinWidthPx = Math.min(Math.max(0, Math.round(minWidthPx)), roundedMaxWidthPx);
  return Math.min(Math.max(Math.round(widthPx), effectiveMinWidthPx), roundedMaxWidthPx);
}

export function initialSidebarWidthForViewport(
  viewportWidthPx: number,
  preference: SidebarSizePreference = defaultSidebarSizePreference,
): number {
  return resolveSidebarWidth(
    preference.desiredWidthPx,
    sidebarResizeBoundsForShellWidth(viewportWidthPx, preference),
  ).px;
}

export function sidebarResizeBoundsForShellWidth(
  shellWidthPx: number,
  preference: SidebarSizePreference = defaultSidebarSizePreference,
): SidebarResizeBounds {
  const roundedShellWidthPx = Math.max(0, Math.round(shellWidthPx));
  const maxWidthPx = Math.round(roundedShellWidthPx * sidebarMaxWidthRatio);
  return {
    maxWidthPx,
    minWidthPx: Math.min(Math.max(sidebarMinWidthPx, Math.round(preference.minWidthPx)), maxWidthPx),
    shellWidthPx: roundedShellWidthPx,
  };
}

export function resolveSidebarWidth(widthPx: number, bounds: SidebarResizeBounds): ResolvedSidebarWidth {
  return { px: clampSidebarWidth(widthPx, bounds.maxWidthPx, bounds.minWidthPx) };
}
