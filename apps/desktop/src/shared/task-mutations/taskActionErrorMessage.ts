import type { TFunction } from "i18next";

import { errorMessage, isTaskContextSelectionRequiredError } from "@/api";

export function taskActionErrorMessage(error: unknown, t: TFunction): string {
  return isTaskContextSelectionRequiredError(error)
    ? t("task.contextSelectionRequired")
    : errorMessage(error);
}
