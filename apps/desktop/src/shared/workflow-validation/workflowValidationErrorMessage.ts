import type { TFunction } from "i18next";

import { ValidationErrorCode, type WorkflowValidationError } from "@/api";

export function workflowValidationErrorMessage(error: WorkflowValidationError, t: TFunction): string {
  if (error.code === ValidationErrorCode.SESSION_SOURCE_CANNOT_OWN_SESSION) {
    return t("workflowEditor.validationSessionSourceCannotOwnSession");
  }
  if (error.code === ValidationErrorCode.SESSION_TRANSITION_MISSING) {
    return t("workflowEditor.validationSessionTransitionMissing");
  }
  if (error.code === ValidationErrorCode.SESSION_TRANSITION_NOT_GUARANTEED) {
    return t("workflowEditor.validationSessionTransitionNotGuaranteed");
  }
  if (error.code === ValidationErrorCode.SESSION_TRANSITION_AMBIGUOUS) {
    return t("workflowEditor.validationSessionTransitionAmbiguous");
  }
  return error.message;
}
