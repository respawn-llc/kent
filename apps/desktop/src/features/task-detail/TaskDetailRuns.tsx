import { useState } from "react";
import { useTranslation } from "react-i18next";

import type { TaskDetail } from "../../api";
import { errorMessage } from "../../api/errors";
import { useAppServices } from "../../app/useAppServices";
import { Button } from "../../ui";

export function RunsTab({ detail, disabled }: Readonly<{ detail: TaskDetail; disabled: boolean }>) {
  const { t } = useTranslation();
  return (
    <section className="grid gap-[var(--space-3)]">
      <TelemetryBox detail={detail} />
      <TeleportBox detail={detail} disabled={disabled} />
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
  return (
    <section className="grid gap-[var(--space-2)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]">
      <h3 className="m-0">{t("task.telemetry")}</h3>
      {detail.worktreePath.length > 0 ? (
        <p className="m-0">
          <strong>{t("task.worktree")}</strong> <span className="font-mono">{detail.worktreePath}</span>
        </p>
      ) : null}
      <p className="m-0">
        <strong>{t("task.status")}</strong> {detail.status.label}
      </p>
    </section>
  );
}

function TeleportBox({ detail, disabled }: Readonly<{ detail: TaskDetail; disabled: boolean }>) {
  const { t } = useTranslation();
  const { api, nativeBridge } = useAppServices();
  const [error, setError] = useState("");
  const terminalAvailable = nativeBridge.capabilities.terminal.launchBuilderSession;

  async function teleport(): Promise<void> {
    const target = await api.getTeleportTarget(detail.id, "");
    if (!target.available) {
      setError(target.failureReason || t("task.teleportUnavailable"));
      return;
    }
    await nativeBridge.terminal.launchBuilderSession({
      sessionId: target.sessionID,
      cwd: teleportCwd(detail.worktreePath, target.cwdRelpath),
    });
  }

  return (
    <section className="grid gap-[var(--space-2)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]">
      <Button
        disabled={disabled || !terminalAvailable}
        onClick={() => {
          void teleport().catch((cause: unknown) => {
            setError(errorMessage(cause));
          });
        }}
        variant="secondary"
      >
        {t("task.teleport")}
      </Button>
      {!terminalAvailable ? (
        <p className="m-0 text-[var(--color-muted)]">{t("task.teleportUnavailable")}</p>
      ) : null}
      {error.length > 0 ? <p className="m-0 text-[var(--color-error)]">{error}</p> : null}
    </section>
  );
}

function teleportCwd(worktreePath: string, cwdRelpath: string): string {
  if (worktreePath.length === 0) {
    return "";
  }
  if (cwdRelpath.length === 0) {
    return worktreePath;
  }
  return `${worktreePath}/${cwdRelpath}`;
}
