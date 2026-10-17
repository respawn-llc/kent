import { FloatingTree, useFloatingTree } from "@floating-ui/react";
import type { ReactNode } from "react";

export function FloatingTreeScope({ children }: Readonly<{ children: ReactNode }>) {
  const tree = useFloatingTree();
  return tree ? <>{children}</> : <FloatingTree>{children}</FloatingTree>;
}
