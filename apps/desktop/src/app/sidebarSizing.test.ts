import {
  clampSidebarWidth,
  initialSidebarWidthForViewport,
  sidebarResizeBoundsForShellWidth,
} from "./sidebarSizing";

describe("sidebar sizing", () => {
  it("uses 35 percent initial width capped at 840px with a 360px minimum", () => {
    expect(initialSidebarWidthForViewport(800)).toBe(360);
    expect(initialSidebarWidthForViewport(1600)).toBe(560);
    expect(initialSidebarWidthForViewport(3000)).toBe(840);
  });

  it("uses 85 percent of shell width as resize max with a 360px minimum", () => {
    expect(sidebarResizeBoundsForShellWidth(320)).toEqual({ maxWidthPx: 360, shellWidthPx: 320 });
    expect(sidebarResizeBoundsForShellWidth(760)).toEqual({ maxWidthPx: 646, shellWidthPx: 760 });
    expect(sidebarResizeBoundsForShellWidth(1200)).toEqual({ maxWidthPx: 1020, shellWidthPx: 1200 });
  });

  it("clamps resized widths to min and max bounds", () => {
    expect(clampSidebarWidth(100, 1020)).toBe(360);
    expect(clampSidebarWidth(560.4, 1020)).toBe(560);
    expect(clampSidebarWidth(1200, 1020)).toBe(1020);
  });
});
