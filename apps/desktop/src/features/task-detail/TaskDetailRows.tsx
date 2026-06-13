import { useId, useMemo, useState, type ReactNode } from "react";
import { Save } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { TaskDetail, TaskRun } from "../../api";
import { errorMessage } from "../../api/errors";
import { useAppServices } from "../../app/useAppServices";
import {
  Button,
  Island,
  Popover,
  PopoverContent,
  PopoverTrigger,
  showStatusToast,
} from "../../ui";
import { cx } from "../../ui/classes";
import { fieldIslandInputClassName } from "../../ui/fieldInputStyles";
import { taskStatusTone } from "./taskStatusTone";
import type { useTaskMutations } from "./useTaskDetailData";

export type TaskDraft = Readonly<{
  title: string;
  body: string;
}>;

export function TaskHeaderIsland({
  detail,
  disabled,
  draft,
  onDraftChange,
  onSave,
}: Readonly<{
  detail: TaskDetail;
  disabled: boolean;
  draft: TaskDraft;
  onDraftChange: (draft: TaskDraft) => void;
  onSave: (draft?: TaskDraft) => Promise<void>;
}>) {
  const { t } = useTranslation();
  const title = draft.title;
  const dirty = draft.title !== detail.title || draft.body !== detail.body;

  function nextTitle(value: string): TaskDraft {
    return { ...draft, title: value.replaceAll("\n", " ") };
  }

  return (
    <form
      className="flex min-w-0 items-center gap-[var(--space-2)]"
      data-testid="task-detail-title-island"
      onSubmit={(event) => {
        event.preventDefault();
        void onSave();
      }}
    >
      <input
        aria-label={t("task.name")}
        className={cx(
          fieldIslandInputClassName(1),
          "min-w-0 flex-1 px-[var(--space-3)] py-[var(--space-2)] text-[1.125rem] font-bold",
        )}
        disabled={disabled}
        onChange={(event) => {
          onDraftChange(nextTitle(event.target.value));
        }}
        onKeyDown={(event) => {
          if (event.key === "Enter") {
            event.preventDefault();
            event.currentTarget.form?.requestSubmit();
          }
        }}
        type="text"
        value={title}
      />
      {dirty ? (
        <Button
          aria-label={t("task.save")}
          className="shrink-0"
          data-testid="task-detail-save"
          disabled={disabled || title.trim().length === 0}
          size="icon"
          type="submit"
          variant="primary"
        >
          <Save aria-hidden="true" size={16} strokeWidth={1.75} />
        </Button>
      ) : null}
    </form>
  );
}

export function DescriptionIsland({
  disabled,
  draft,
  error,
  onDraftChange,
}: Readonly<{
  disabled: boolean;
  draft: TaskDraft;
  error: unknown;
  onDraftChange: (draft: TaskDraft) => void;
}>) {
  const { t } = useTranslation();
  const descriptionId = useId();
  const descriptionErrorId = `${descriptionId}-error`;
  const descriptionError = error == null ? "" : errorMessage(error);
  return (
    <div className="grid min-h-0 min-w-0 gap-[var(--space-2)]" data-testid="task-description-input-frame">
      <textarea
        aria-describedby={descriptionError.length > 0 ? descriptionErrorId : undefined}
        aria-invalid={descriptionError.length > 0 ? true : undefined}
        aria-label={t("task.description")}
        className={cx(
          fieldIslandInputClassName(1),
          "block min-h-[220px] min-w-0 resize-none p-[var(--space-2)]",
        )}
        disabled={disabled}
        id={descriptionId}
        onChange={(event) => {
          onDraftChange({ ...draft, body: event.target.value });
        }}
        placeholder={t("task.bodyPlaceholder")}
        value={draft.body}
      />
      {descriptionError.length > 0 ? (
        <span className="text-[var(--color-error)]" id={descriptionErrorId}>
          {descriptionError}
        </span>
      ) : null}
    </div>
  );
}

export function PropertiesIsland({
  detail,
  disabled,
  mutations,
  resumeRunId,
}: Readonly<{
  detail: TaskDetail;
  disabled: boolean;
  mutations: ReturnType<typeof useTaskMutations>;
  resumeRunId: string;
}>) {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();
  const [openCliError, setOpenCliError] = useState("");
  const cliSessionExists = useMemo(
    () => detail.runs.some((run) => run.sessionID.trim().length > 0),
    [detail.runs],
  );
  const cliCommand = useMemo(() => sessionCommand(detail.runs), [detail.runs]);
  const activeRuns = useMemo(
    () => detail.runs.filter((run) => run.completedAt === 0 && run.interruptedAt === 0),
    [detail.runs],
  );
  const resumeID = resumeRunId.length > 0 ? resumeRunId : detail.actions.resumeRunID;

  async function openInCli(): Promise<void> {
    if (cliCommand.length === 0) {
      setOpenCliError(t("task.cliCommandUnavailable"));
      return;
    }
    await copyText(cliCommand, nativeBridge);
    showStatusToast({
      id: "task-cli-command-copied",
      title: t("task.cliCommandCopied"),
      tone: "success",
    });
  }

  return (
    <Island
      aria-label={t("task.properties")}
      className="grid min-w-0 content-start gap-[var(--space-2)] p-[var(--space-4)]"
      level={1}
      unpadded
    >
      <PropertyLine label={t("task.identifier", { defaultValue: "ID" })} value={<span className="font-mono">{detail.shortID}</span>} />
      <PropertyLine label={t("task.project")} value={detail.projectName} />
      <PropertyLine
        label={t("task.status")}
        value={<TaskStatusText label={detail.status.label} tone={taskStatusTone(detail.status)} />}
      />
      <PropertyLine label={t("task.workspace")} value={detail.sourceWorkspace.name} />
      <PropertyLine label={t("task.workflow")} value={detail.workflowName} />
      <PropertyLine label={t("task.sessions")} value={detail.runs.length.toString()} />
      <div className="grid gap-[var(--space-2)] pt-[var(--space-1)]">
        {cliSessionExists ? (
          <Button
            disabled={disabled || cliCommand.length === 0}
            onClick={() => {
              setOpenCliError("");
              void openInCli().catch((cause: unknown) => {
                setOpenCliError(errorMessage(cause));
              });
            }}
            variant="secondary"
          >
            {t("task.openInCli")}
          </Button>
        ) : null}
        {detail.actions.canResume ? (
          <Button
            disabled={disabled}
            onClick={() => {
              void mutations.resume.mutateAsync(resumeID);
            }}
            variant="primary"
          >
            {t("board.resume")}
          </Button>
        ) : null}
        {detail.actions.canInterrupt
          ? activeRuns.map((run) => (
              <Button
                disabled={disabled}
                key={run.id}
                onClick={() => {
                  void mutations.interrupt.mutateAsync(run.id);
                }}
                variant="secondary"
              >
                {t("board.interrupt")} <span className="font-mono">{run.id}</span>
              </Button>
            ))
          : null}
        {detail.actions.canCancel ? (
          <Popover>
            <PopoverTrigger asChild>
              <Button disabled={disabled} variant="danger">
                {t("task.cancel")}
              </Button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-56" side="top">
              <strong>{t("task.cancelConfirmTitle")}</strong>
              <Button
                disabled={disabled}
                onClick={() => {
                  void mutations.cancel.mutateAsync();
                }}
                variant="danger"
              >
                {t("app.confirm")}
              </Button>
            </PopoverContent>
          </Popover>
        ) : null}
      </div>
      {openCliError.length > 0 ? (
        <p className="m-0 text-sm text-[var(--color-error)]">{openCliError}</p>
      ) : null}
    </Island>
  );
}

function TaskStatusText({ label, tone }: Readonly<{ label: string; tone: ReturnType<typeof taskStatusTone> }>) {
  return <span className={cx("font-bold", taskStatusTextClassName(tone))}>{label}</span>;
}

function taskStatusTextClassName(tone: ReturnType<typeof taskStatusTone>): string {
  if (tone === "info") {
    return "text-[var(--color-primary)]";
  }
  if (tone === "success") {
    return "text-[var(--color-success)]";
  }
  if (tone === "warning") {
    return "text-[var(--color-warning)]";
  }
  if (tone === "danger") {
    return "text-[var(--color-error)]";
  }
  return "text-[var(--color-muted)]";
}

function PropertyLine({ label, value }: Readonly<{ label: string; value: ReactNode }>) {
  return (
    <p className="m-0 flex min-w-0 flex-wrap items-center gap-[var(--space-1)] text-sm">
      {label}: <span className="text-[var(--color-muted)]">{value}</span>
    </p>
  );
}

async function copyText(
  value: string,
  nativeBridge: ReturnType<typeof useAppServices>["nativeBridge"],
): Promise<void> {
  if (nativeBridge.capabilities.clipboard.writeText) {
    await nativeBridge.clipboard.writeText(value);
    return;
  }
  await navigator.clipboard.writeText(value);
}

function sessionCommand(runs: readonly TaskRun[]): string {
  const run = preferredSessionRun(runs);
  return run === null ? "" : `kent --session=${run.sessionID}`;
}

function preferredSessionRun(runs: readonly TaskRun[]): TaskRun | null {
  const sessionRuns = runs.filter((run) => run.sessionID.trim().length > 0);
  return (
    [...sessionRuns].reverse().find((run) => run.completedAt === 0 && run.interruptedAt === 0) ??
    sessionRuns.at(-1) ??
    null
  );
}
