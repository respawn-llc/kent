import type { ComponentProps } from "react";
import { cx } from "./classes";

export function ScrollRegion({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      {...props}
      className={cx("min-h-0 min-w-0 overflow-y-auto overflow-x-hidden overscroll-contain", className)}
    />
  );
}
