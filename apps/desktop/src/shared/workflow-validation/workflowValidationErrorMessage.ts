import type { TFunction } from "i18next";

import { workflowValidationErrorReason, type WorkflowValidationError } from "@/api";

export function workflowValidationErrorMessage(error: WorkflowValidationError, t: TFunction): string {
  switch (error.details.reason) {
    case workflowValidationErrorReason.SESSION_SOURCE_CANNOT_OWN_SESSION:
      return t("workflowEditor.validationSessionSourceCannotOwnSession");
    case workflowValidationErrorReason.SESSION_TRANSITION_MISSING:
      return t("workflowEditor.validationSessionTransitionMissing");
    case workflowValidationErrorReason.SESSION_TRANSITION_NOT_GUARANTEED:
      return t("workflowEditor.validationSessionTransitionNotGuaranteed");
    case workflowValidationErrorReason.SESSION_TRANSITION_AMBIGUOUS:
      return t("workflowEditor.validationSessionTransitionAmbiguous");
    case workflowValidationErrorReason.UNSPECIFIED:
      return error.message;
    case null:
      return error.message;
  }
}
