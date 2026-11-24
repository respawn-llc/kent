import type { TFunction } from "i18next";

import { errorMessage, isTaskContextSelectionRequiredError } from "@/api";

export function taskActionErrorMessage(error: unknown, t: TFunction): string {
  if (isTaskContextSelectionRequiredError(error)) return t("task.contextSelectionRequired");
  return errorMessage(error);
}
