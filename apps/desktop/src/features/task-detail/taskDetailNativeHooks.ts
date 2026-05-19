import { useEffect, useState } from "react";

import { useAppServices } from "../../app/useAppServices";

export function useNativeTaskDetailTarget(taskId: string, resumeRunId: string) {
  const { logger, nativeBridge } = useAppServices();
  const [target, setTarget] = useState({ resumeRunId, taskId });

  useEffect(() => {
    setTarget({ resumeRunId, taskId });
  }, [resumeRunId, taskId]);

  useEffect(() => {
    let active = true;
    let unlisten: (() => void) | null = null;
    void nativeBridge.taskDetail.onOpen((nextTarget) => {
      if (active) {
        setTarget(nextTarget);
      }
    }).then((listener) => {
      if (!active) {
        listener();
        return;
      }
      unlisten = listener;
    }).catch((cause: unknown) => {
      void logger.append("warn", "Task detail retarget listener failed.", {
        error: cause instanceof Error ? cause.message : "unknown",
      });
    });
    return () => {
      active = false;
      unlisten?.();
    };
  }, [logger, nativeBridge.taskDetail]);

  return target;
}
