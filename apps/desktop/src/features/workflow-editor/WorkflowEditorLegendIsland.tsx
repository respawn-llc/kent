import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { CircleQuestionMark } from "lucide-react";

import { FloatingNoticeIsland } from "../../ui";
import { cx } from "../../ui/classes";

export function WorkflowEditorLegendIsland({
  positionStrategy,
}: Readonly<{ positionStrategy: "absolute" | "fixed" }>) {
  const { t } = useTranslation();
  const [collapsed, setCollapsed] = useState(true);
  return (
    <FloatingNoticeIsland
      collapsed={collapsed}
      collapseLabel={t("app.collapse")}
      expandedClassName="floating-notice-expanded grid h-[204px] w-[min(300px,calc(100vw-var(--space-2)*2))] gap-[6px] rounded-[var(--radius-xl)] p-[var(--space-2)]"
      expandLabel={t("app.expand")}
      icon={
        <CircleQuestionMark
          aria-hidden="true"
          data-testid="workflow-legend-collapsed-help-icon"
          size={24}
          strokeWidth={1.7}
        />
      }
      level={3}
      onCollapsedChange={setCollapsed}
      positionClassName="left-[var(--space-2)] bottom-[var(--space-2)]"
      positionStrategy={positionStrategy}
      title={t("workflowEditor.legend")}
      tone="neutral"
    >
      <div className="grid gap-[6px] pt-[4px] text-sm leading-none text-[var(--color-on-island)]">
        <LegendRow label={t("workflowEditor.legendContinueSession")}>
          <EdgeLegendSwatch tone="neutral" />
        </LegendRow>
        <LegendRow label={t("workflowEditor.legendFreshSession")}>
          <EdgeLegendSwatch tone="primary" />
        </LegendRow>
        <LegendRow label={t("workflowEditor.legendCompactSession")}>
          <EdgeLegendSwatch tone="secondary" />
        </LegendRow>
        <LegendRow label={t("workflowEditor.legendAgentNode")}>
          <NodeLegendSwatch tone="neutral" />
        </LegendRow>
        <LegendRow label={t("workflowEditor.legendTerminalState")}>
          <NodeLegendSwatch tone="success" />
        </LegendRow>
        <LegendRow label={t("workflowEditor.legendStartingState")}>
          <NodeLegendSwatch tone="primary" />
        </LegendRow>
        <LegendRow label={t("workflowEditor.legendMultiAgentJoin")}>
          <NodeLegendSwatch shape="diamond" tone="secondary" />
        </LegendRow>
      </div>
    </FloatingNoticeIsland>
  );
}

function LegendRow({ children, label }: Readonly<{ children: ReactNode; label: string }>) {
  return (
    <div className="grid grid-cols-[26px_minmax(0,1fr)] items-center gap-[var(--space-2)]">
      <span className="grid h-3 place-items-center">{children}</span>
      <span className="min-w-0">{label}</span>
    </div>
  );
}

function EdgeLegendSwatch({ tone }: Readonly<{ tone: "neutral" | "primary" | "secondary" }>) {
  return (
    <svg
      aria-hidden="true"
      className={edgeLegendToneClassName(tone)}
      data-testid="workflow-legend-edge-swatch"
      fill="none"
      height="6"
      viewBox="0 0 22 6"
      width="22"
    >
      <path
        d="M1 3H19"
        data-testid="workflow-legend-edge-line"
        stroke="currentColor"
        strokeLinecap="round"
        strokeWidth="1.25"
      />
      <path
        d="M17 1L20 3L17 5"
        data-testid="workflow-legend-edge-head"
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="1.25"
      />
    </svg>
  );
}

function NodeLegendSwatch({
  shape = "box",
  tone,
}: Readonly<{ shape?: "box" | "diamond"; tone: "neutral" | "primary" | "secondary" | "success" }>) {
  const shapeClassName =
    shape === "diamond" ? "h-[10px] w-[10px] rotate-45 rounded-[2px]" : "h-[9px] w-[14px] rounded-[2px]";
  return (
    <span
      aria-hidden="true"
      className={cx("block border bg-[var(--color-island-1)]", shapeClassName, nodeLegendToneClassName(tone))}
      data-testid="workflow-legend-node-swatch"
    />
  );
}

function edgeLegendToneClassName(tone: "neutral" | "primary" | "secondary"): string {
  if (tone === "primary") {
    return "text-[var(--color-primary)]";
  }
  if (tone === "secondary") {
    return "text-[var(--color-secondary)]";
  }
  return "text-[var(--color-muted)]";
}

function nodeLegendToneClassName(tone: "neutral" | "primary" | "secondary" | "success"): string {
  if (tone === "primary") {
    return "border-[var(--color-primary)]";
  }
  if (tone === "secondary") {
    return "border-[var(--color-secondary)]";
  }
  if (tone === "success") {
    return "border-[var(--color-success)]";
  }
  return "border-[var(--color-outline)]";
}
