import { createElement, forwardRef, type HTMLAttributes, type ReactNode } from "react";

import { cx } from "./classes";
import { islandSurfaceClassName, type IslandLevel } from "./islandSurfaceStyles";

export type IslandSurfaceProps = Readonly<{
  as?: IslandSurfaceElement;
  children: ReactNode;
  level?: IslandLevel;
}> &
  HTMLAttributes<HTMLElement>;

export type IslandSurfaceElement = "article" | "aside" | "div" | "footer" | "header" | "section";

export const IslandSurface = forwardRef<HTMLElement, IslandSurfaceProps>(function IslandSurface(
  { as: Component = "div", children, className, level = 0, ...props },
  ref,
) {
  return createElement(
    Component,
    { className: cx(islandSurfaceClassName(level), className), ref, ...props },
    children,
  );
});
