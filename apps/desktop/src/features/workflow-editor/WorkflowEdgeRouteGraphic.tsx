import { ArrowRight } from "lucide-react";
import { useTranslation } from "react-i18next";

import { workflowEdgeColor } from "./workflowGraphColors";

export type WorkflowEdgeRouteGraphicProps = Readonly<{
  contextMode: string;
  hasError?: boolean | undefined;
  sourceLabel: string;
  targetLabel: string;
}>;

export function WorkflowEdgeRouteGraphic({
  contextMode,
  hasError = false,
  sourceLabel,
  targetLabel,
}: WorkflowEdgeRouteGraphicProps) {
  const { t } = useTranslation();
  return (
    <div
      aria-label={t("workflowEditor.edgeRouteAriaLabel", { source: sourceLabel, target: targetLabel })}
      className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-[var(--space-2)]"
      data-testid="workflow-edge-route-graphic"
      role="group"
    >
      <WorkflowEdgeRouteGraphicNode label={sourceLabel} testID="workflow-edge-route-source" />
      <ArrowRight
        aria-hidden="true"
        className="size-5 shrink-0"
        data-testid="workflow-edge-route-arrow"
        size={20}
        strokeWidth={1.8}
        style={{ color: workflowEdgeColor(contextMode, hasError) }}
      />
      <WorkflowEdgeRouteGraphicNode label={targetLabel} testID="workflow-edge-route-target" />
    </div>
  );
}

function WorkflowEdgeRouteGraphicNode({ label, testID }: Readonly<{ label: string; testID: string }>) {
  return (
    <span
      className="grid min-h-10 min-w-0 place-items-center px-[var(--space-2)] py-[var(--space-2)] text-center text-sm font-semibold text-[var(--color-on-island)]"
      data-testid={testID}
    >
      <span className="block max-w-full truncate">{label}</span>
    </span>
  );
}
