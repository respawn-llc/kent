import { useMemo } from "react";

import { useOwnedSidebarRoots } from "@/app-facade";
import { useStableCallback } from "@/ui";
import { goalSidebarDestination } from "./goalSidebarDestination";
import type { GoalSidebarInput } from "./GoalSidebar";

export function useGoalSidebarLauncher(input: GoalSidebarInput): () => void {
  const roots = useOwnedSidebarRoots();
  const target = input.kind === "session" ? input.target.sessionID : input.binding;
  const identity = useMemo(() => Symbol("goal-sidebar"), [target]);
  return useStableCallback(() => {
    roots.toggle(goalSidebarDestination(input, identity));
  });
}
