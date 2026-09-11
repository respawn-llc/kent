import type { TFunction } from "i18next";

import type { WorkflowValidationError } from "@/api";

export function workflowValidationErrorMessage(error: WorkflowValidationError, t: TFunction): string {
  switch (error.details.reason) {
    case "session_source_cannot_own_session":
      return t("workflowEditor.validationSessionSourceCannotOwnSession");
    case "session_transition_missing":
      return t("workflowEditor.validationSessionTransitionMissing");
    case "session_transition_not_guaranteed":
      return t("workflowEditor.validationSessionTransitionNotGuaranteed");
    case "session_transition_ambiguous":
      return t("workflowEditor.validationSessionTransitionAmbiguous");
    case null:
      return error.message;
  }
}
