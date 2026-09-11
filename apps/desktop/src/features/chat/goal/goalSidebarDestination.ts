import { createElement } from "react";

import type { SidebarDestination } from "@/app-facade";
import { GoalSidebarPage, type GoalSidebarInput } from "./GoalSidebar";

export function goalSidebarDestination(input: GoalSidebarInput): SidebarDestination {
  return {
    kind: "custom",
    sizing: { desiredWidthPx: 560, minWidthPx: 400 },
    title: "Goal",
    content: createElement(GoalSidebarPage, { input }),
  };
}
