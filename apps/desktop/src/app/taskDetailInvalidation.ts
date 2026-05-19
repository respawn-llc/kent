import type { QueryClient } from "@tanstack/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

import { queryKeys } from "./queryKeys";
import { useAppServices } from "./useAppServices";

export function useTaskDetailMutationInvalidator() {
  const queryClient = useQueryClient();
  const { logger, nativeBridge } = useAppServices();

  useEffect(() => {
    let active = true;
    let unlisten: (() => void) | null = null;
    void nativeBridge.taskDetail.onChanged(() => {
      if (!active) {
        return;
      }
      void refreshVisibleTaskQueries(queryClient);
    }).then((listener) => {
      if (!active) {
        listener();
        return;
      }
      unlisten = listener;
    }).catch((cause: unknown) => {
      void logger.append("warn", "Task detail mutation listener failed.", {
        error: cause instanceof Error ? cause.message : "unknown",
      });
    });
    return () => {
      active = false;
      unlisten?.();
    };
  }, [logger, nativeBridge.taskDetail, queryClient]);
}

async function refreshVisibleTaskQueries(queryClient: QueryClient): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: queryKeys.projects }),
    queryClient.invalidateQueries({ queryKey: queryKeys.allAttention }),
    queryClient.invalidateQueries({ queryKey: queryKeys.allBoards }),
    queryClient.invalidateQueries({ queryKey: queryKeys.allTasks }),
    queryClient.invalidateQueries({ queryKey: queryKeys.allActivity }),
    queryClient.invalidateQueries({ queryKey: queryKeys.allPendingAsks }),
  ]);
}
