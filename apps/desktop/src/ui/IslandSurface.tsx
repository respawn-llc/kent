import type { HTMLAttributes, ReactNode } from "react";

import { cx } from "./classes";
import { islandSurfaceClassName, type IslandLevel } from "./islandSurfaceStyles";

export type IslandSurfaceProps = Readonly<{
  as?: IslandSurfaceElement;
  children: ReactNode;
  level?: IslandLevel;
}> &
  HTMLAttributes<HTMLElement>;

export type IslandSurfaceElement = "article" | "aside" | "div" | "footer" | "header" | "section";

export function IslandSurface({
  as: Component = "div",
  children,
  className,
  level = 0,
  ...props
}: IslandSurfaceProps) {
  return (
    <Component className={cx(islandSurfaceClassName(level), className)} {...props}>
      {children}
    </Component>
  );
}
