import type { TFunction } from "i18next";

import type { WorkflowValidationError } from "@/api";

export function workflowValidationErrorMessage(error: WorkflowValidationError, t: TFunction): string {
  switch (error.code) {
    case "workflow.validation.session_source_cannot_own_session":
      return t("workflowEditor.validationSessionSourceCannotOwnSession");
    case "workflow.validation.session_transition_missing":
      return t("workflowEditor.validationSessionTransitionMissing");
    case "workflow.validation.session_transition_not_guaranteed":
      return t("workflowEditor.validationSessionTransitionNotGuaranteed");
    case "workflow.validation.session_transition_ambiguous":
      return t("workflowEditor.validationSessionTransitionAmbiguous");
    default:
      return error.message;
  }
}
