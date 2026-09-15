import { useCallback, useEffect, useRef } from "react";

import { useOwnedSidebarRoots } from "@/app-facade";
import { goalSidebarDestination } from "./goalSidebarDestination";
import type { GoalSidebarInput } from "./GoalSidebar";

export function useGoalSidebarLauncher(input: GoalSidebarInput): () => void {
  const { open } = useOwnedSidebarRoots();
  const active = useRef<Readonly<{
    key: string | object;
    generation: number;
  }> | null>(null);
  const key = input.kind === "session" ? `session:${input.target.sessionID}` : input.binding;

  useEffect(
    () => () => {
      active.current = null;
    },
    [],
  );

  return useCallback(() => {
    const current = active.current;
    if (current?.key === key) {
      return;
    }
    const generation = (current?.generation ?? 0) + 1;
    const handle = open(goalSidebarDestination(input));
    active.current = {
      key,
      generation,
    };
    void handle.lifecycle.then(
      () => {
        if (active.current?.generation === generation && active.current.key === key) {
          active.current = null;
        }
      },
      () => {
        if (active.current?.generation === generation && active.current.key === key) {
          active.current = null;
        }
      },
    );
  }, [input, key, open]);
}
