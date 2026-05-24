import { useTranslation } from "react-i18next";

import type { WorkflowRecord } from "../../api";
import { HomeListCard } from "../../ui";

export function WorkflowCard({
  onOpen,
  workflow,
}: Readonly<{ onOpen: () => void; workflow: WorkflowRecord }>) {
  const { t } = useTranslation();
  return (
    <HomeListCard ariaLabel={`${workflow.name} rev ${workflow.graphRevision.toString()}`} onClick={onOpen}>
      <span className="font-mono text-[0.78rem] text-[var(--color-muted)]">rev {workflow.graphRevision}</span>
      <strong>{workflow.name}</strong>
      <span className="truncate text-sm text-[var(--color-muted)]">
        {workflow.description.length > 0 ? workflow.description : t("workflowLibrary.reusableDefinition")}
      </span>
    </HomeListCard>
  );
}
