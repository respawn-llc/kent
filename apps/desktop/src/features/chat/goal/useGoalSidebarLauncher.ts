import { useCallback, useEffect, useRef } from "react";

import { useOwnedSidebarRoots } from "@/app-facade";
import { goalSidebarDestination } from "./goalSidebarDestination";
import type { GoalSidebarInput } from "./GoalSidebar";

export function useGoalSidebarLauncher(input: GoalSidebarInput): () => void {
  const { open } = useOwnedSidebarRoots();
  const active = useRef<Readonly<{
    key: string | object;
    generation: number;
    close(): void;
  }> | null>(null);
  const key = input.kind === "session" ? `session:${input.target.sessionID}` : input.binding;

  useEffect(
    () => () => {
      active.current?.close();
      active.current = null;
    },
    [],
  );

  return useCallback(() => {
    const current = active.current;
    if (current?.key === key) {
      return;
    }
    current?.close();
    const generation = (current?.generation ?? 0) + 1;
    const handle = open(goalSidebarDestination(input));
    active.current = {
      key,
      generation,
      close: () => undefined,
    };
    void handle.lifecycle.then(
      () => {
        if (active.current?.generation === generation && active.current.key === key) {
          active.current.close();
          active.current = null;
        }
      },
      () => {
        if (active.current?.generation === generation && active.current.key === key) {
          active.current.close();
          active.current = null;
        }
      },
    );
  }, [input, key, open]);
}
