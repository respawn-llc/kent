import type { TFunction } from "i18next";

import { errorMessage, isTaskContextSelectionRequiredError, TaskExecutionError } from "@/api";

export function taskActionErrorMessage(error: unknown, t: TFunction): string {
  if (isTaskContextSelectionRequiredError(error)) return t("task.contextSelectionRequired");
  if (error instanceof TaskExecutionError && error.detail.case === "internalFailure") {
    return error.detail.value.cause ?? errorMessage(error);
  }
  return errorMessage(error);
}
