import { useEffect, useState } from "react";

import { useAppServices } from "../../app/useAppServices";

export function useNativeTaskDetailTarget(taskId: string, resumeRunId: string) {
  const { logger, nativeBridge } = useAppServices();
  const [nativeTarget, setNativeTarget] = useState<Readonly<{ resumeRunId: string; taskId: string }> | null>(null);

  useEffect(() => {
    let active = true;
    let unlisten: (() => void) | null = null;
    void nativeBridge.taskDetail.onOpen((nextTarget) => {
      if (active) {
        setNativeTarget(nextTarget);
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

  return nativeTarget ?? { resumeRunId, taskId };
}
