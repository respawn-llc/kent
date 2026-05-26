import { useTranslation } from "react-i18next";

import type { TaskDetail } from "../../api";
import { formatHomeRelativePath } from "../../app/formatters";
import { useAppServices } from "../../app/useAppServices";
import { Badge } from "../../ui";
import { taskStatusTone } from "./taskStatusTone";

export function RunsTab({ detail }: Readonly<{ detail: TaskDetail }>) {
  const { t } = useTranslation();
  return (
    <section className="grid gap-[var(--space-3)]">
      <TelemetryBox detail={detail} />
      {detail.runs.length === 0 ? <p>{t("task.noRuns")}</p> : null}
      {detail.runs.map((run) => (
        <article
          className="grid gap-[var(--space-1)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]"
          key={run.id}
        >
          <span className="font-mono">{run.id}</span>
          <span>{run.status}</span>
          <span className="font-mono text-sm text-[var(--color-muted)]">{run.sessionID}</span>
          <span className="text-sm text-[var(--color-muted)]">{run.role}</span>
        </article>
      ))}
    </section>
  );
}

function TelemetryBox({ detail }: Readonly<{ detail: TaskDetail }>) {
  const { t } = useTranslation();
  const { homePath, nativeBridge } = useAppServices();
  const worktreePathLabel = formatHomeRelativePath(
    detail.worktreePath,
    homePath,
    nativeBridge.capabilities.platform,
  );
  return (
    <section className="grid gap-[var(--space-2)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]">
      <h3 className="m-0">{t("task.telemetry")}</h3>
      {detail.worktreePath.length > 0 ? (
        <p className="m-0">
          <strong>{t("task.worktree")}</strong>{" "}
          <span className="font-mono" title={detail.worktreePath}>
            {worktreePathLabel}
          </span>
        </p>
      ) : null}
      <p className="m-0 flex flex-wrap items-center gap-[var(--space-1)]">
        <strong>{t("task.status")}</strong> <Badge tone={taskStatusTone(detail.status)}>{detail.status.label}</Badge>
      </p>
    </section>
  );
}
