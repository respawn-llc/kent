import { useTranslation } from "react-i18next";

import type { WorkflowValidationError } from "../../api";

export type WorkflowValidationIssuesProps = Readonly<{
  errors: readonly WorkflowValidationError[];
}>;

export function WorkflowValidationIssues({ errors }: WorkflowValidationIssuesProps) {
  const { t } = useTranslation();
  const messages =
    errors.length > 0 ? errors.map((issue) => issue.message) : [t("board.invalidWorkflowUnknown")];
  return (
    <ul className="workflow-issues-list m-0 grid max-w-[72ch] list-none gap-[var(--space-1)] p-0 text-sm leading-snug text-[var(--color-on-island)]">
      {messages.map((message) => (
        <li className="relative pl-[1.2rem]" key={message}>
          <span>{message}</span>
        </li>
      ))}
    </ul>
  );
}
