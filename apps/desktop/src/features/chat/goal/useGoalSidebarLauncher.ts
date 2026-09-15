import { useCallback, useEffect, useRef } from "react";

import { useOwnedSidebarRoots } from "@/app-facade";
import { createNewChatGoalResource } from "./goalBinding";
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
    const resource = input.kind === "new_chat" ? createNewChatGoalResource() : undefined;
    const unregister =
      input.kind === "new_chat" && resource !== undefined
        ? input.binding.registerResource(resource)
        : undefined;
    const destination = input.kind === "new_chat" && resource !== undefined ? { ...input, resource } : input;
    const handle = (() => {
      try {
        return open(goalSidebarDestination(destination));
      } catch (error) {
        unregister?.();
        resource?.close();
        throw error;
      }
    })();
    active.current = {
      key,
      generation,
      close: () => {
        unregister?.();
        resource?.close();
      },
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
