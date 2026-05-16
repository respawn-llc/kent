import { useState } from "react";
import { useTranslation } from "react-i18next";

import type { AttentionItem, TaskComment, TaskDetail, TaskRun, TaskTransition } from "../../api";
import { errorMessage } from "../../api/errors";
import { formatRelativeTime } from "../../app/formatters";
import { useOpenExternalLink } from "../../app/nativeHooks";
import { useAppServices } from "../../app/useAppServices";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { Badge, Button, Dialog, ErrorState, MarkdownText, SelectField, TextArea, TextInput } from "../../ui";
import { useUpdateTask, useWorkspaces } from "../tasks/useTaskMutations";
import { usePendingAsks, useTaskActivity, useTaskDetail, useTaskMutations } from "./useTaskDetailData";

export type TaskDetailDialogProps = Readonly<{
  taskId: string;
  open: boolean;
  resumeRunId: string;
  onClose: () => void;
}>;

export function TaskDetailDialog({ taskId, open, resumeRunId, onClose }: TaskDetailDialogProps) {
  const { t } = useTranslation();
  const detail = useTaskDetail(taskId, open);
  const activity = useTaskActivity(taskId, open);
  const openLink = useOpenExternalLink();

  return (
    <Dialog className="task-detail-dialog" closeLabel={t("app.close")} onClose={onClose} open={open} title={t("task.title")}>
      {detail.isPending ? <p>{t("states.loading")}</p> : null}
      {detail.isError ? <ErrorState body={errorMessage(detail.error)} title={t("states.error")} /> : null}
      {detail.data !== undefined ? (
        <TaskDetailContent detail={detail.data} openLink={openLink} resumeRunId={resumeRunId} />
      ) : null}
      {activity.data !== undefined ? (
        <ActivityFeed
          items={activity.data.pages.flatMap((page) => page.items)}
          loading={activity.isFetchingNextPage}
          onLoadMore={() => void activity.fetchNextPage()}
          showMore={activity.hasNextPage}
        />
      ) : null}
    </Dialog>
  );
}

function TaskDetailContent({ detail, openLink, resumeRunId }: Readonly<{ detail: TaskDetail; openLink: (url: string) => void; resumeRunId: string }>) {
  const { t } = useTranslation();
  const mutations = useTaskMutations(detail.id);
  const connection = useConnectionSnapshot();
  const disabled = connection.phase !== "connected";
  const question = detail.attention.find((item) => item.kind === "question");
  const approval = detail.attention.find((item) => item.kind === "approval");
  const activeRuns = detail.runs.filter((run) => run.completedAt === 0 && run.interruptedAt === 0);

  return (
    <div className="task-detail stack">
      <header className="task-detail__identity">
        <div>
          <p className="mono">{t("task.shortId", { id: detail.shortID })}</p>
          <h2>{detail.title}</h2>
          <p>{detail.projectName} · {detail.workflowName}</p>
        </div>
        <div className="chip-row">
          <Badge tone="info">{detail.status.label}</Badge>
          <Badge tone="neutral">{detail.sourceWorkspace.name}</Badge>
        </div>
      </header>
      <MarkdownText onOpenLink={openLink} value={detail.body} />
      <TaskEditForm detail={detail} disabled={disabled} key={`${detail.id}-${detail.updatedAt.toString()}`} />
      <TaskActions activeRuns={activeRuns} detail={detail} disabled={disabled} resumeRunId={resumeRunId} />
      {question !== undefined ? <QuestionBox attention={question} disabled={disabled} mutations={mutations} taskId={detail.id} /> : null}
      {approval !== undefined ? <ApprovalBox attention={approval} disabled={disabled} mutations={mutations} transitions={detail.transitions} /> : null}
      <TelemetryBox detail={detail} />
      <TeleportBox detail={detail} disabled={disabled} />
      <Comments comments={detail.comments} disabled={disabled} mutations={mutations} openLink={openLink} />
    </div>
  );
}

function TaskEditForm({ detail, disabled }: Readonly<{ detail: TaskDetail; disabled: boolean }>) {
  const { t } = useTranslation();
  const editable = detail.status.kind === "backlog" && detail.runs.length === 0 && detail.worktreePath.length === 0 && !detail.done && detail.canceledAt === 0;
  const workspaces = useWorkspaces(detail.projectID);
  const update = useUpdateTask(detail.id);
  const [title, setTitle] = useState(detail.title);
  const [body, setBody] = useState(detail.body);
  const [workspaceID, setWorkspaceID] = useState(detail.sourceWorkspace.id);

  if (!editable) {
    return <p>{t("task.sourceWorkspaceLocked")}</p>;
  }

  async function save(): Promise<void> {
    await update.mutateAsync({ taskID: detail.id, title, body, sourceWorkspaceID: workspaceID });
  }

  return (
    <form className="inline-form" onSubmit={(event) => { event.preventDefault(); void save(); }}>
      <TextInput label={t("task.name")} onChange={(event) => { setTitle(event.target.value); }} value={title} />
      <TextArea label={t("task.body")} onChange={(event) => { setBody(event.target.value); }} rows={4} value={body} />
      <SelectField label={t("task.sourceWorkspace")} onChange={(event) => { setWorkspaceID(event.target.value); }} value={workspaceID}>
        {(workspaces.data?.workspaces ?? []).map((workspace) => (
          <option key={workspace.id} value={workspace.id}>{workspace.name}</option>
        ))}
      </SelectField>
      {update.error !== null ? <p className="form-error">{errorMessage(update.error)}</p> : null}
      <Button disabled={disabled || update.isPending} type="submit" variant="primary">{t("task.save")}</Button>
    </form>
  );
}

function TaskActions({ activeRuns, detail, disabled, resumeRunId }: Readonly<{ activeRuns: readonly TaskRun[]; detail: TaskDetail; disabled: boolean; resumeRunId: string }>) {
  const { t } = useTranslation();
  const [confirmCancel, setConfirmCancel] = useState(false);
  const mutations = useTaskMutations(detail.id);
  const resumeID = resumeRunId.length > 0 ? resumeRunId : detail.actions.resumeRunID;

  return (
    <div className="action-strip">
      {detail.actions.canResume ? (
        <Button disabled={disabled} onClick={() => void mutations.resume.mutateAsync(resumeID)} variant="primary">{t("board.resume")}</Button>
      ) : null}
      {activeRuns.map((run) => (
        <Button disabled={disabled} key={run.id} onClick={() => void mutations.interrupt.mutateAsync(run.id)} variant="secondary">
          {t("board.interrupt")} <span className="mono">{run.id}</span>
        </Button>
      ))}
      {detail.actions.canCancel ? (
        <Button disabled={disabled} onClick={() => { setConfirmCancel(true); }} variant="danger">{t("task.cancel")}</Button>
      ) : null}
      {confirmCancel ? (
        <div className="confirm-inline">
          <strong>{t("task.cancelConfirmTitle")}</strong>
          <p>{t("task.cancelConfirmBody")}</p>
          <Button disabled={disabled} onClick={() => void mutations.cancel.mutateAsync()} variant="danger">{t("app.confirm")}</Button>
        </div>
      ) : null}
    </div>
  );
}

function QuestionBox({ attention, disabled, mutations, taskId }: Readonly<{ attention: AttentionItem; disabled: boolean; mutations: ReturnType<typeof useTaskMutations>; taskId: string }>) {
  const { t } = useTranslation();
  const asks = usePendingAsks(attention.sessionID);
  const pendingAsk = asks.data?.find((ask) => ask.askID === attention.askID);
  const [answer, setAnswer] = useState("");
  const [selectedOption, setSelectedOption] = useState(0);

  async function submit(): Promise<void> {
    await mutations.answerQuestion.mutateAsync({
      clientRequestID: `gui-question-${attention.askID}-${Date.now().toString()}`,
      taskID: taskId,
      runID: attention.runID,
      askID: attention.askID,
      selectedOptionNumber: selectedOption,
      freeformAnswer: answer,
    });
    setAnswer("");
    setSelectedOption(0);
  }

  return (
    <section className="attention-box">
      <h3>{t("task.question")}</h3>
      <p>{pendingAsk?.question ?? attention.message}</p>
      <div className="option-list">
        {(pendingAsk?.suggestions ?? []).map((suggestion, optionIndex) => (
          <button className={selectedOption === optionIndex + 1 ? "selected-option" : ""} key={suggestion} onClick={() => { setSelectedOption(optionIndex + 1); }} type="button">
            {suggestion} {pendingAsk?.recommendedOptionIndex === optionIndex + 1 ? t("task.recommended") : ""}
          </button>
        ))}
      </div>
      <TextArea label={t("task.answer")} onChange={(event) => { setAnswer(event.target.value); }} placeholder={t("task.answerPlaceholder")} rows={3} value={answer} />
      <Button disabled={disabled || mutations.answerQuestion.isPending || (answer.trim().length === 0 && selectedOption === 0)} onClick={() => void submit()} variant="primary">
        {t("task.submitAnswer")}
      </Button>
    </section>
  );
}

function ApprovalBox({ attention, disabled, mutations, transitions }: Readonly<{ attention: AttentionItem; disabled: boolean; mutations: ReturnType<typeof useTaskMutations>; transitions: readonly TaskTransition[] }>) {
  const { t } = useTranslation();
  const transition = transitions.find((item) => item.id === attention.taskTransitionID);
  return (
    <section className="attention-box">
      <h3>{t("task.approval")}</h3>
      <p>{attention.message}</p>
      {transition !== undefined ? (
        <dl className="detail-grid">
          <dt>{t("task.approvalSnapshot")}</dt>
          <dd>{transition.sourceNodeName} · {transition.transitionName || transition.transitionID}</dd>
          <dt>{t("task.outputValues")}</dt>
          <dd>{Object.entries(transition.outputValues).map(([key, value]) => `${key}: ${value}`).join("\n")}</dd>
          <dt>{t("app.version")}</dt>
          <dd>{transition.graphRevision}</dd>
        </dl>
      ) : <p>{t("task.unavailableSnapshot")}</p>}
      <Button disabled={disabled || mutations.approve.isPending} onClick={() => void mutations.approve.mutateAsync(attention.taskTransitionID)} variant="primary">
        {t("task.approve")}
      </Button>
    </section>
  );
}

function TelemetryBox({ detail }: Readonly<{ detail: TaskDetail }>) {
  const { t } = useTranslation();
  return (
    <section className="detail-grid">
      <h3>{t("task.runs")}</h3>
      {detail.runs.length === 0 ? <p>{t("task.noRuns")}</p> : null}
      {detail.runs.map((run) => (
        <p key={run.id}><span className="mono">{run.id}</span> {run.status} {run.sessionID}</p>
      ))}
      {detail.worktreePath.length > 0 ? <p><strong>{t("task.worktree")}</strong> <span className="mono">{detail.worktreePath}</span></p> : null}
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
    <section className="teleport-box">
      <Button disabled={disabled || !terminalAvailable} onClick={() => void teleport().catch((cause: unknown) => { setError(errorMessage(cause)); })} variant="secondary">
        {t("task.teleport")}
      </Button>
      {!terminalAvailable ? <p>{t("task.teleportUnavailable")}</p> : null}
      {error.length > 0 ? <p className="form-error">{error}</p> : null}
    </section>
  );
}

function Comments({ comments, disabled, mutations, openLink }: Readonly<{ comments: readonly TaskComment[]; disabled: boolean; mutations: ReturnType<typeof useTaskMutations>; openLink: (url: string) => void }>) {
  const { t } = useTranslation();
  const [body, setBody] = useState("");
  const [editing, setEditing] = useState<Readonly<{ id: string; body: string }> | null>(null);

  async function submit(): Promise<void> {
    if (editing === null) {
      await mutations.addComment.mutateAsync(body);
      setBody("");
      return;
    }
    await mutations.replaceComment.mutateAsync({ commentID: editing.id, body: editing.body });
    setEditing(null);
  }

  return (
    <section className="comments stack">
      <h3>{t("task.comments")}</h3>
      <TextArea label={editing === null ? t("task.addComment") : t("task.editComment")} onChange={(event) => {
        if (editing === null) {
          setBody(event.target.value);
          return;
        }
        setEditing({ id: editing.id, body: event.target.value });
      }} rows={3} value={editing?.body ?? body} />
      <Button disabled={disabled || (editing?.body ?? body).trim().length === 0} onClick={() => void submit()} variant="primary">{editing === null ? t("task.addComment") : t("task.save")}</Button>
      {comments.map((comment) => (
        <article className="comment-row" key={comment.id}>
          <MarkdownText onOpenLink={openLink} value={comment.body} />
          <span>{formatRelativeTime(comment.createdAt)}</span>
          <Button disabled={disabled} onClick={() => { setEditing({ id: comment.id, body: comment.body }); }} variant="ghost">{t("task.editComment")}</Button>
          <Button disabled={disabled} onClick={() => void mutations.deleteComment.mutateAsync(comment.id)} variant="danger">{t("task.deleteComment")}</Button>
        </article>
      ))}
    </section>
  );
}

function ActivityFeed({ items, loading, onLoadMore, showMore }: Readonly<{ items: readonly { id: string; summary: string; occurredAt: number }[]; loading: boolean; onLoadMore: () => void; showMore: boolean }>) {
  const { t } = useTranslation();
  return (
    <section className="activity-feed stack">
      <h3>{t("task.activity")}</h3>
      {items.length === 0 ? <p>{t("task.noActivityTitle")}</p> : null}
      {items.map((item) => (
        <article className="activity-row" key={item.id}>
          <span>{item.summary}</span>
          <time>{formatRelativeTime(item.occurredAt)}</time>
        </article>
      ))}
      {showMore ? <Button disabled={loading} onClick={onLoadMore} variant="ghost">{loading ? t("app.loadingMore") : t("task.loadOlderActivity")}</Button> : null}
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
