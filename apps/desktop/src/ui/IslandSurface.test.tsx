import { render, screen } from "@testing-library/react";

import { IslandSurface } from "./IslandSurface";
import { islandSurfaceClassName } from "./islandSurfaceStyles";

describe("IslandSurface", () => {
  it("uses the level-0 surface without elevated blur", () => {
    render(
      <IslandSurface as="section" data-testid="base-island" level={0}>
        Base island
      </IslandSurface>,
    );

    expect(screen.getByTestId("base-island")).toHaveClass("island-surface", "island-surface-0");
    expect(screen.getByTestId("base-island")).not.toHaveClass("island-surface-1");
  });

  it("uses elevated level classes for blurred island layers", () => {
    render(
      <IslandSurface data-testid="elevated-island" level={3}>
        Elevated island
      </IslandSurface>,
    );

    expect(screen.getByTestId("elevated-island")).toHaveClass("island-surface", "island-surface-3");
  });

  it("returns reusable surface class names", () => {
    expect(islandSurfaceClassName(4)).toBe("island-surface island-surface-4");
  });
});
