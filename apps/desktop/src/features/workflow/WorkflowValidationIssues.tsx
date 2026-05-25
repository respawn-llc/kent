import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";

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
          details: workflowValidationErrorDetails(issue, t),
          message: issue.message,
        }))
      : [{ details: [], id: "unknown", message: t("board.invalidWorkflowUnknown") }];
  return (
    <ul className="workflow-issues-list m-0 grid max-w-[72ch] list-none gap-[var(--space-1)] p-0 text-sm leading-snug text-[var(--color-on-island)]">
      {items.map((item) => (
        <li className="relative pl-[1.2rem]" key={item.id}>
          <span>{item.message}</span>
          <WorkflowValidationErrorDetailsText details={item.details} as="span" />
        </li>
      ))}
    </ul>
  );
}

export function WorkflowValidationErrorDetailsLine({
  error,
}: Readonly<{ error: WorkflowValidationError }>) {
  const { t } = useTranslation();
  return <WorkflowValidationErrorDetailsText details={workflowValidationErrorDetails(error, t)} as="p" />;
}

export function workflowValidationErrorDetails(
  error: WorkflowValidationError,
  t: TFunction,
): readonly string[] {
  return [
    detail(t("workflowEditor.validationDetailInput"), error.details.inputName),
    detail(t("workflowEditor.validationDetailField"), error.details.fieldName),
    detail(t("workflowEditor.validationDetailPlaceholder"), error.details.placeholder),
    detail(t("workflowEditor.validationDetailProviderEdge"), error.details.providerEdgeID),
  ].filter((item): item is string => item !== null);
}

function detail(label: string, value: string): string | null {
  return value.length === 0 ? null : `${label}: ${value}`;
}

function WorkflowValidationErrorDetailsText({
  as,
  details,
}: Readonly<{ as: "p" | "span"; details: readonly string[] }>) {
  if (details.length === 0) {
    return null;
  }
  const className =
    as === "p"
      ? "m-0 mt-[var(--space-1)] font-mono text-xs text-[var(--color-muted)]"
      : "block font-mono text-xs text-[var(--color-muted)]";
  return as === "p" ? (
    <p className={className}>{details.join(" · ")}</p>
  ) : (
    <span className={className}>{details.join(" · ")}</span>
  );
}
