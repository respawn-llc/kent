import { useId, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import type { WorkflowValidationError } from "../../api";
import { ErrorState, IslandSurface } from "../../ui";
import { WorkflowValidationErrorDetailsLine } from "../workflow/WorkflowValidationIssues";

export function DetailSection({
  children,
  hideTitle = false,
  leading,
  title,
}: Readonly<{
  children: ReactNode;
  hideTitle?: boolean | undefined;
  leading?: ReactNode | undefined;
  title?: string | undefined;
}>) {
  const titleID = useId();
  return (
    <IslandSurface
      aria-labelledby={title === undefined ? undefined : titleID}
      as="section"
      className="grid gap-[var(--space-2)] rounded-[var(--radius-l)] p-[var(--space-3)]"
      level={1}
    >
      {leading}
      {title === undefined ? null : (
        <h3 className={hideTitle ? "sr-only" : "m-0 text-sm font-bold"} id={titleID}>
          {title}
        </h3>
      )}
      {children}
    </IslandSurface>
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
      <ul className="m-0 grid list-disc gap-[var(--space-1)] pl-[1.1rem] text-sm leading-snug">
        {errors.map((error, index) => (
          <li className="pl-[2px] marker:text-[var(--color-error)]" key={validationErrorKey(error, index)}>
            <span>{error.message}</span>
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
    <ErrorState
      body={t("workflowEditor.entityMissing")}
      details={entityID}
      fullPage={false}
      reveal={false}
      title={t("workflowEditor.inspectorUnavailable")}
    />
  );
}
