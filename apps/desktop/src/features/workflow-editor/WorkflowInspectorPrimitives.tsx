import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import type { WorkflowValidationError } from "../../api";
import { Badge } from "../../ui";
import { WorkflowValidationErrorDetailsLine } from "../workflow/WorkflowValidationIssues";

export function DetailSection({ children, title }: Readonly<{ children: ReactNode; title?: string }>) {
  return (
    <section className="grid gap-[var(--space-2)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]">
      {title === undefined ? null : <h3 className="m-0 text-sm font-bold">{title}</h3>}
      {children}
    </section>
  );
}

export function DetailRow({
  label,
  mono = false,
  value,
}: Readonly<{ label: string; mono?: boolean; value: string }>) {
  return (
    <div className="grid gap-[2px]">
      <span className="text-xs font-bold uppercase tracking-[0.14em] text-[var(--color-muted)]">{label}</span>
      <span className={mono ? "break-all font-mono text-sm" : "text-sm"}>{value}</span>
    </div>
  );
}

export function InspectorStack({ children }: Readonly<{ children: ReactNode }>) {
  return <div className="grid gap-[var(--space-3)]">{children}</div>;
}

export function ValidationDetails({
  errors,
  title,
}: Readonly<{ errors: readonly WorkflowValidationError[]; title?: string }>) {
  const { t } = useTranslation();
  if (errors.length === 0) {
    return null;
  }
  return (
    <DetailSection title={title ?? t("workflowEditor.validationErrors")}>
      <ul className="m-0 grid gap-[var(--space-2)] p-0">
        {errors.map((error, index) => (
          <li
            className="list-none rounded-[var(--radius-m)] border border-[var(--color-error)] bg-[color-mix(in_srgb,var(--color-error)_12%,transparent)] p-[var(--space-2)]"
            key={validationErrorKey(error, index)}
          >
            <div className="mb-[var(--space-1)]">
              <Badge tone={error.blocksContext ? "danger" : "warning"}>{error.code}</Badge>
            </div>
            <p className="m-0 text-sm">{error.message}</p>
            <WorkflowValidationErrorDetailsLine error={error} />
          </li>
        ))}
      </ul>
    </DetailSection>
  );
}

function validationErrorKey(error: WorkflowValidationError, index: number): string {
  const entityID = error.edgeID || error.nodeID || error.transitionGroupID || error.workflowID;
  if (entityID.length > 0) {
    return `${error.code}:${entityID}:${error.message}`;
  }
  return `${error.code}:${error.message}:${index.toString()}`;
}

export function MissingEntity({ entityID }: Readonly<{ entityID: string }>) {
  const { t } = useTranslation();
  return (
    <InspectorStack>
      <DetailSection title={t("workflowEditor.inspectorUnavailable")}>
        <p className="m-0 text-sm text-[var(--color-muted)]">{t("workflowEditor.entityMissing")}</p>
        <code className="font-mono text-sm">{entityID}</code>
      </DetailSection>
    </InspectorStack>
  );
}
