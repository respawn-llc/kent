import type { TFunction } from "i18next";
import type { ApprovalDecision } from "@/api";

export function approvalDecisionLabel(decision: ApprovalDecision, t: TFunction): string {
  switch (decision) {
    case "allow_once":
      return t("task.approvalDecisionAllowOnce");
    case "allow_session":
      return t("task.approvalDecisionAllowSession");
    case "deny":
      return t("task.approvalDecisionDeny");
  }
}
