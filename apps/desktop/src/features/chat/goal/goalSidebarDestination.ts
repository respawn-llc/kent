import { createElement } from "react";

import type { SidebarDestination } from "@/app-facade";
import { GoalSidebarPage, type GoalSidebarInput } from "./GoalSidebar";

export function goalSidebarDestination(input: GoalSidebarInput, identity: symbol): SidebarDestination {
  return {
    kind: "custom",
    identity,
    sizing: { desiredWidthPx: 560, minWidthPx: 400 },
    title: "Goal",
    content: () => createElement(GoalSidebarPage, { input }),
  };
}
