import { useState, type MouseEvent } from "react";
import { useTaskSpeech } from "./TaskDetailSpeech";
import { ControlTooltip } from "@/ui";
import { Save } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { TaskDetail } from "@/api";
import { errorMessage, sessionDisplayID } from "@/api";
import {
  useAppServices,
  useNativeChatOpening,
  usePathFormatter,
  useTextFieldSubmitShortcut,
  useTextFieldSubmitShortcutPolicy,
  type SessionChatOpeningIntent,
} from "@/app-facade";
import { useOpenExternalLink } from "@/app-facade";
import { writeClipboardText } from "@/shared/native-clipboard";
import { taskStatusTone } from "@/shared/task-status";
import {
  Button,
  CollapsibleMarkdownField,
  Island,
  compactExternalUrlLabel,
  safeExternalUrl,
  showStatusToast,
  Spinner,
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/ui";
import { cx, fieldIslandInputClassName } from "@/ui";
import type { DescriptionPresentationState } from "./TaskDetailDescriptionPresentation";
import type { TaskDetailSessionChatEntry } from "./taskDetailSessionChat";
import { TaskExecutionTargetFacts } from "./TaskExecutionTargetFacts";
import { TaskDetailLabels } from "./TaskDetailLabels";
import { TaskDeleteButton } from "./TaskDeleteButton";
import { TaskResumeButton, TaskStartButton } from "./TaskResumeButton";
import { TaskPropertyLine } from "./TaskPropertyLine";
import { taskDetailIslandRadius } from "./taskDetailIslandStyles";
import { taskExecutionRoot } from "./taskExecutionTarget";
import type { TaskDetailLifecycle } from "./TaskDetailLifecycleActions";

export type TaskDraft = Readonly<{
  title: string;
  body: string;
}>;
export type SaveTaskDraft = (draft?: TaskDraft, onSaved?: () => void) => void;

function TaskSessionOpenButton({
  pending,
  label,
  tooltip,
  className,
  onClick,
}: Readonly<{
  pending: boolean;
  label: string;
  tooltip: string;
  className?: string;
  onClick(event: MouseEvent<HTMLButtonElement>): void;
}>) {
  return (
    <Button
      aria-label={label}
      className={className}
      disabled={pending}
      onClick={onClick}
      title={tooltip}
      variant="secondary"
    >
      {pending && <Spinner size="sm" />}
      <span className="min-w-0 truncate">{label}</span>
    </Button>
  );
}

function TaskSessionOpenAction({
  sessions,
  onOpen,
  label,
  defaultIntent,
  nativeWindowsAvailable,
  pendingSessionID,
}: Readonly<{
  sessions: TaskDetail["liveSessions"];
  onOpen(sessionID: string, intent: SessionChatOpeningIntent): void;
  label: string;
  defaultIntent: SessionChatOpeningIntent;
  nativeWindowsAvailable: boolean;
  pendingSessionID: string | null;
}>) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [pickerIntent, setPickerIntent] = useState(defaultIntent);
  const triggerPending = pendingSessionID !== null;
  const actionTooltip = (action: string) =>
    nativeWindowsAvailable ? t("app.shiftClickToOpenInNewWindow", { action }) : action;
  const clickIntent = (event: MouseEvent<HTMLButtonElement>) =>
    event.shiftKey && nativeWindowsAvailable ? "native_window" : defaultIntent;
  const only = sessions.length === 1 ? sessions[0] : undefined;
  if (only !== undefined) {
    const tooltip = actionTooltip(label);
    return (
      <TaskSessionOpenButton
        pending={pendingSessionID === only.sessionID}
        label={label}
        tooltip={tooltip}
        onClick={(event) => {
          onOpen(only.sessionID, clickIntent(event));
        }}
      />
    );
  }
  if (sessions.length === 0) return null;
  const tooltip = actionTooltip(label);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          disabled={triggerPending}
          onClick={(event) => {
            setPickerIntent(clickIntent(event));
          }}
          title={tooltip}
        >
          {triggerPending && <Spinner size="sm" />}
          <span className="min-w-0 truncate">{label}</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        collisionPadding={12}
        level={3}
        className="w-[min(26rem,calc(100vw-24px))] gap-0 p-[var(--space-2)]"
      >
        <div className="grid max-h-[min(20rem,var(--radix-popover-content-available-height))] min-w-0 gap-[var(--space-1)] overflow-y-auto">
          {sessions.map((session) => {
            const sessionLabel = session.sessionName ?? sessionDisplayID(session.sessionID, session.shortID);
            const sessionAction = t("task.openChat", { name: sessionLabel });
            return (
              <TaskSessionOpenButton
                key={session.sessionID}
                pending={pendingSessionID === session.sessionID}
                label={sessionLabel}
                tooltip={actionTooltip(sessionAction)}
                className="w-full justify-start rounded-[var(--radius-s)] border-0 bg-transparent text-left font-sans text-sm hover:bg-[var(--color-island-2)]"
                onClick={(event) => {
                  const intent = event.shiftKey && nativeWindowsAvailable ? "native_window" : pickerIntent;
                  setOpen(false);
                  onOpen(session.sessionID, intent);
                }}
              />
            );
          })}
        </div>
      </PopoverContent>
    </Popover>
  );
}

export function TaskHeaderIsland({
  canSaveDraft,
  detail,
  saving,
  draft,
  onDraftChange,
  onSave,
}: Readonly<{
  canSaveDraft: boolean;
  detail: TaskDetail;
  saving: boolean;
  draft: TaskDraft;
  onDraftChange: (draft: TaskDraft) => void;
  onSave: SaveTaskDraft;
}>) {
  const { t } = useTranslation();
  const { description: speech } = useTaskSpeech();
  const title = draft.title;
  const dirty = draft.title !== detail.title || draft.body !== detail.body;
  const formShortcut = useTextFieldSubmitShortcut({
    available: canSaveDraft,
    kind: "form",
  });

  function nextTitle(value: string): TaskDraft {
    return { ...draft, title: value.replaceAll("\n", " ") };
  }

  return (
    <form
      className="flex min-w-0 items-center gap-[var(--space-2)]"
      data-testid="task-detail-title-island"
      onKeyDown={formShortcut}
      onSubmit={(event) => {
        event.preventDefault();
        if (canSaveDraft) {
          onSave();
        }
      }}
    >
      <input
        aria-label={t("task.name")}
        className={cx(
          fieldIslandInputClassName(1, taskDetailIslandRadius),
          "min-w-0 flex-1 px-[var(--space-3)] py-[var(--space-2)] text-[1.125rem] font-bold",
        )}
        onChange={(event) => {
          onDraftChange(nextTitle(event.target.value));
        }}
        onKeyDown={(event) => {
          if (event.key === "Enter" && !event.metaKey && !event.ctrlKey) {
            event.preventDefault();
            event.currentTarget.form?.requestSubmit();
          }
        }}
        type="text"
        value={title}
      />
      {dirty || detail.actions.canDelete ? (
        <span className="relative block h-9 w-9 shrink-0" data-testid="task-detail-title-action-slot">
          <ControlTooltip
            label={speech.busy ? speech.busyReason : t("task.save")}
            className="absolute inset-0"
          >
            <Button
              aria-label={t("task.save")}
              aria-hidden={!dirty}
              className={`absolute inset-0 transition-opacity motion-reduce:transition-none ${
                dirty
                  ? "pointer-events-auto opacity-100"
                  : "pointer-events-none opacity-0 disabled:opacity-0!"
              }`}
              data-testid="task-detail-save"
              disabled={!canSaveDraft || !dirty}
              aria-busy={saving}
              size="icon"
              tabIndex={dirty ? undefined : -1}
              type="submit"
              variant="primary"
            >
              {saving ? <Spinner /> : <Save aria-hidden="true" size={16} strokeWidth={1.75} />}
            </Button>
          </ControlTooltip>
          {detail.actions.canDelete ? <TaskDeleteButton active={!dirty} disabled={false} /> : null}
        </span>
      ) : null}
    </form>
  );
}

export function DescriptionIsland({
  draft,
  draftDirty,
  error,
  onDraftChange,
  onPresentationChange,
  onSave,
  presentation,
  submitting,
}: Readonly<{
  draft: TaskDraft;
  draftDirty: boolean;
  error: unknown;
  onDraftChange: (draft: TaskDraft) => void;
  onPresentationChange: (presentation: DescriptionPresentationState) => void;
  onSave: SaveTaskDraft;
  presentation: DescriptionPresentationState;
  submitting: boolean;
}>) {
  const { t } = useTranslation();
  const { description: speech } = useTaskSpeech();
  const descriptionError = error == null ? undefined : errorMessage(error);
  const submitPolicy = useTextFieldSubmitShortcutPolicy();
  const submitIntent = {
    available: !submitting && !speech.busy && draft.title.trim().length > 0 && presentation.editing,
    onSubmitIntent: () => {
      if (!draftDirty) {
        onPresentationChange({ ...presentation, editing: false });
        return;
      }
      onSave(draft, () => {
        onPresentationChange({ ...presentation, editing: false });
      });
    },
    policy: submitPolicy,
  } as const;

  return (
    <div
      className="task-detail-description-island grid h-full w-full min-h-0 min-w-0 max-w-full"
      data-testid="task-description-input-frame"
    >
      <CollapsibleMarkdownField
        editorRef={speech.editorRef}
        floatingAction={speech.control}
        collapsedHeightClamp={{ kind: "lines", maximumLines: 10, minimumLines: 5, viewportPercent: 50 }}
        editorMinHeight={220}
        error={descriptionError}
        editing={presentation.editing}
        expandLabel={t("app.expand")}
        expanded={presentation.expanded}
        label={t("task.description")}
        onChange={(body) => {
          onDraftChange({ ...draft, body });
        }}
        onEdit={() => {
          onPresentationChange({ editing: true, expanded: true });
        }}
        onEditingChange={(editing) => {
          onPresentationChange({ ...presentation, editing });
        }}
        onExpand={() => {
          onPresentationChange({ ...presentation, expanded: true });
        }}
        placeholder={t("task.bodyPlaceholder")}
        submitIntent={submitIntent}
        surfaceRadius={taskDetailIslandRadius}
        taskListInteraction={{
          checkedLabel: t("markdown.markIncomplete"),
          uncheckedLabel: t("markdown.markComplete"),
        }}
        value={draft.body}
      />
    </div>
  );
}

export function PropertiesIsland({
  detail,
  mutations,
  openSessionChat,
  sessionChatDefaultIntent,
}: Readonly<{
  detail: TaskDetail;
  mutations: TaskDetailLifecycle;
  openSessionChat?: TaskDetailSessionChatEntry | undefined;
  sessionChatDefaultIntent: SessionChatOpeningIntent;
}>) {
  const { t } = useTranslation();
  const openExternalLink = useOpenExternalLink();
  return (
    <Island
      aria-label={t("task.properties")}
      className="grid min-w-0 content-start gap-[var(--space-2)] p-[var(--space-4)]"
      level={1}
      radius={taskDetailIslandRadius}
      unpadded
    >
      <dl className="m-0 grid min-w-0 gap-[var(--space-2)]">
        <TaskPropertyLine
          label={t("task.identifier", { defaultValue: "ID" })}
          value={<span className="font-mono">{detail.shortID}</span>}
        />
        <TaskDetailLabels />
        <TaskPropertyLine label={t("task.project")} value={detail.projectName} />
        <TaskPropertyLine
          label={t("task.status")}
          value={
            <TaskStatusText
              label={t(`task.statusKinds.${detail.status.kind}`)}
              tone={taskStatusTone(detail.status)}
            />
          }
        />
        <TaskExecutionTargetFacts detail={detail} />
        <TaskPropertyLine label={t("task.workflow")} value={detail.workflowName} />
        <SourceLine label={t("task.source")} onOpen={openExternalLink} value={detail.sourceURL} />
        <TaskPropertyLine label={t("task.sessions")} value={detail.retainedSessionCount.toString()} />
        {detail.currentNodes.map((node) => (
          <TaskCurrentNodeSelectionProperties key={node.nodeID} node={node} />
        ))}
      </dl>
      <TaskActionPanel
        detail={detail}
        mutations={mutations}
        openSessionChat={openSessionChat}
        sessionChatDefaultIntent={sessionChatDefaultIntent}
      />
    </Island>
  );
}

export function TaskCurrentNodeSelectionProperties({
  node,
}: Readonly<{ node: TaskDetail["currentNodes"][number] }>) {
  const { t } = useTranslation();
  return (
    <>
      {node.effectiveAssignee === null ? null : (
        <TaskPropertyLine
          label={t("task.currentNodeAssignee", { nodeName: node.displayName })}
          value={<span className="font-mono">{node.effectiveAssignee}</span>}
        />
      )}
      {node.effectiveThinking === null ? null : (
        <TaskPropertyLine
          label={t("task.currentNodeThinking", { nodeName: node.displayName })}
          value={<span className="font-mono">{node.effectiveThinking}</span>}
        />
      )}
    </>
  );
}

function TaskActionPanel({
  detail,
  mutations,
  openSessionChat,
  sessionChatDefaultIntent,
}: Readonly<{
  detail: TaskDetail;
  mutations: TaskDetailLifecycle;
  openSessionChat?: TaskDetailSessionChatEntry | undefined;
  sessionChatDefaultIntent: SessionChatOpeningIntent;
}>) {
  const { t } = useTranslation();
  const interruptFullLabel = t("board.interrupt");
  return (
    <>
      <div
        className="flex min-w-0 flex-wrap items-center justify-start gap-[var(--space-2)] pt-[var(--space-1)]"
        data-testid="task-detail-action-flow"
      >
        {detail.actions.canStart ? (
          <TaskStartButton />
        ) : detail.actions.canResume ? (
          <TaskResumeButton />
        ) : null}
        <TaskOpenButtons
          detail={detail}
          openSessionChat={openSessionChat}
          sessionChatDefaultIntent={sessionChatDefaultIntent}
        />
        {detail.actions.canInterrupt ? (
          <Button
            aria-label={interruptFullLabel}
            disabled={mutations.interruptPending}
            onClick={() => {
              mutations.interrupt();
            }}
            title={interruptFullLabel}
            variant="danger"
          >
            {interruptFullLabel}
          </Button>
        ) : null}
      </div>
    </>
  );
}

function TaskOpenButtons({
  detail,
  openSessionChat,
  sessionChatDefaultIntent,
}: Readonly<{
  detail: TaskDetail;
  openSessionChat?: TaskDetailSessionChatEntry | undefined;
  sessionChatDefaultIntent: SessionChatOpeningIntent;
}>) {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();
  const formatPath = usePathFormatter();
  const [openError, setOpenError] = useState("");
  const nativeChat = useNativeChatOpening();
  const executionRoot = taskExecutionRoot(detail);
  const canOpenScript = nativeBridge.capabilities.files.open;
  const canOpenChat = openSessionChat !== undefined || sessionChatDefaultIntent === "native_window";
  const nativeWindowsAvailable = canOpenChat && nativeBridge.capabilities.dialogWindows;

  async function openInCli(sessionID: string): Promise<void> {
    await writeClipboardText(`kent --session=${sessionID}`, nativeBridge);
    showStatusToast({
      id: "task-cli-command-copied",
      title: t("task.cliCommandCopied"),
      tone: "success",
    });
  }

  async function openScript(path: string): Promise<void> {
    await nativeBridge.files.openFile({ basePath: executionRoot, path });
  }

  return (
    <>
      <TaskSessionOpenAction
        sessions={detail.liveSessions}
        label={t(canOpenChat ? "task.openInChat" : "task.openInCli")}
        defaultIntent={sessionChatDefaultIntent}
        nativeWindowsAvailable={nativeWindowsAvailable}
        pendingSessionID={nativeChat.pendingSessionID}
        onOpen={(sessionID, intent) => {
          setOpenError("");
          if (intent === "native_window") {
            nativeChat.open({ projectID: detail.projectID, sessionID });
            return;
          }
          const action =
            !canOpenChat || openSessionChat === undefined
              ? openInCli(sessionID)
              : openSessionChat({ projectID: detail.projectID, sessionID });
          void action.catch((cause: unknown) => {
            setOpenError(errorMessage(cause));
          });
        }}
      />
      {canOpenScript
        ? detail.currentScripts.map((script) => (
            <Button
              key={`${script.currentNode.nodeID}:${script.currentNode.transitionBranchKey ?? "serial"}`}
              onClick={() => {
                setOpenError("");
                void openScript(script.path).catch((cause: unknown) => {
                  setOpenError(errorMessage(cause));
                });
              }}
              variant="secondary"
            >
              {t("task.openScript")} <span className="truncate font-mono">{formatPath(script.path)}</span>
            </Button>
          ))
        : null}
      {openError.length > 0 ? <p className="m-0 text-sm text-[var(--color-error)]">{openError}</p> : null}
    </>
  );
}

function TaskStatusText({
  label,
  tone,
}: Readonly<{ label: string; tone: ReturnType<typeof taskStatusTone> }>) {
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

function SourceLine({
  label,
  onOpen,
  value,
}: Readonly<{ label: string; onOpen: (url: string) => void; value: string | null }>) {
  if (value === null) return null;
  const trimmed = value.trim();
  if (trimmed.length === 0) {
    return null;
  }
  const href = safeExternalUrl(trimmed);
  if (href === undefined) {
    return <TaskPropertyLine label={label} value={trimmed} />;
  }
  return (
    <TaskPropertyLine
      label={label}
      value={
        <a
          className="truncate text-[var(--color-primary)] underline-offset-2 hover:underline"
          data-testid="task-source-link"
          href={href}
          onClick={(event) => {
            event.preventDefault();
            onOpen(href);
          }}
          rel="noreferrer"
          target="_blank"
          title={href}
        >
          {compactExternalUrlLabel(href)}
        </a>
      }
    />
  );
}
