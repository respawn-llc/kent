import type { TFunction } from "i18next";
import type { TaskDeleteResponse } from "@/api";
import type { StatusController } from "@/app-facade";
import { branchCleanupWarnings } from "@/shared/workspaces";

export function notifyTaskDeletionCleanup(
  result: TaskDeleteResponse,
  push: StatusController["push"],
  t: TFunction,
): void {
  const warnings = branchCleanupWarnings(result.branchCleanup, t);
  if (warnings.length > 0)
    push({
      id: crypto.randomUUID(),
      tone: "warning",
      title: t("board.deleteTask"),
      body: warnings.join("\n"),
    });
}
