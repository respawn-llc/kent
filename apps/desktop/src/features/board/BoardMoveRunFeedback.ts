import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";

import { useStatusController } from "../../app/useStatusController";

export type BoardMoveRunTracker = Readonly<{
  observeInterruptedRun: (input: Readonly<{ runID: string; taskID: string }>) => void;
  trackMoveRunIDs: (result: Readonly<{ runIDs: readonly string[] }>) => void;
}>;

export function useBoardMoveRunFeedback(): BoardMoveRunTracker {
  const { t } = useTranslation();
  const { push } = useStatusController();
  const [pendingMoveRunIDs, setPendingMoveRunIDs] = useState<ReadonlySet<string>>(() => new Set());

  const trackMoveRunIDs = useCallback((result: Readonly<{ runIDs: readonly string[] }>): void => {
    const runIDs = result.runIDs.map((runID) => runID.trim()).filter((runID) => runID.length > 0);
    if (runIDs.length === 0) {
      return;
    }
    setPendingMoveRunIDs((current) => new Set([...current, ...runIDs]));
  }, []);

  const observeInterruptedRun = useCallback(
    (input: Readonly<{ runID: string; taskID: string }>): void => {
      const runID = input.runID.trim();
      if (runID.length === 0 || !pendingMoveRunIDs.has(runID)) {
        return;
      }
      setPendingMoveRunIDs((current) => {
        if (!current.has(runID)) {
          return current;
        }
        const next = new Set(current);
        next.delete(runID);
        return next;
      });
      push({
        id: `board-move-run-interrupted-${runID}`,
        tone: "danger",
        title: t("board.moveRunInterrupted"),
        body: t("board.moveRunInterruptedBody", { runID, taskID: input.taskID }),
        dismissible: false,
      });
    },
    [pendingMoveRunIDs, push, t],
  );

  return { observeInterruptedRun, trackMoveRunIDs };
}
