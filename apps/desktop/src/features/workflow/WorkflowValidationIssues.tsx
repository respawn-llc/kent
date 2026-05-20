import { useTranslation } from "react-i18next";

import type { WorkflowValidationError } from "../../api";

export type WorkflowValidationIssuesProps = Readonly<{
  errors: readonly WorkflowValidationError[];
}>;

export function WorkflowValidationIssues({ errors }: WorkflowValidationIssuesProps) {
  const { t } = useTranslation();
  const items =
    errors.length > 0
      ? errors.map((issue, index) => ({
          id: `${issue.code}-${issue.workflowID}-${issue.nodeID}-${issue.transitionGroupID}-${issue.edgeID}-${index.toString()}`,
          message: issue.message,
        }))
      : [{ id: "unknown", message: t("board.invalidWorkflowUnknown") }];
  return (
    <ul className="workflow-issues-list m-0 grid max-w-[72ch] list-none gap-[var(--space-1)] p-0 text-sm leading-snug text-[var(--color-on-island)]">
      {items.map((item) => (
        <li className="relative pl-[1.2rem]" key={item.id}>
          <span>{item.message}</span>
        </li>
      ))}
    </ul>
  );
}
