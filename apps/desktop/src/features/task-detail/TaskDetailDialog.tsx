import { useTranslation } from "react-i18next";

import { errorMessage } from "../../api/errors";
import { useOpenExternalLink } from "../../app/nativeHooks";
import { Dialog, ErrorState, LoadingState } from "../../ui";
import { TaskDetailContent } from "./TaskDetailContent";
import { taskDetailContentMaxWidth, taskDetailDialogOuterMaxWidth } from "./taskDetailLayout";
import { useTaskActivity, useTaskDetail } from "./useTaskDetailData";

export type TaskDetailDialogProps = Readonly<{
  taskId: string;
  open: boolean;
  resumeRunId: string;
  onClose: () => void;
  onMutated?: (() => void) | undefined;
}>;

export function TaskDetailDialog({ taskId, open, resumeRunId, onClose, onMutated }: TaskDetailDialogProps) {
  const { t } = useTranslation();

  return (
    <Dialog
      className="h-[min(860px,calc(100vh-32px))] w-[calc(100vw-32px)]"
      chrome="floating-close"
      closeLabel={t("app.close")}
      contentPadding="chrome"
      onClose={onClose}
      open={open}
      surface="transparent"
      style={{ maxWidth: taskDetailDialogOuterMaxWidth }}
      title={t("task.title")}
    >
      <div
        className="mx-auto h-full min-h-0 w-full"
        data-testid="task-detail-dialog-content"
        style={{ maxWidth: taskDetailContentMaxWidth }}
      >
        <TaskDetailSurface enabled={open} onMutated={onMutated} resumeRunId={resumeRunId} taskId={taskId} />
      </div>
    </Dialog>
  );
}

export type TaskDetailSurfaceProps = Readonly<{
  taskId: string;
  enabled: boolean;
  initialFocus?: "firstQuestion" | undefined;
  resumeRunId: string;
  onMutated?: (() => void) | undefined;
}>;

export function TaskDetailSurface({
  taskId,
  enabled,
  initialFocus,
  resumeRunId,
  onMutated,
}: TaskDetailSurfaceProps) {
  const { t } = useTranslation();
  const detail = useTaskDetail(taskId, enabled);
  const activity = useTaskActivity(taskId, enabled);
  const openLink = useOpenExternalLink();

  if (detail.isPending) {
    return <LoadingState appearanceDelayMs={0} fullPage={false} reveal={false} title={t("states.loading")} />;
  }
  if (detail.isError) {
    return <ErrorState body={errorMessage(detail.error)} reveal={false} title={t("states.error")} />;
  }
  return (
    <TaskDetailContent
      activity={activity}
      detail={detail.data}
      initialFocus={initialFocus}
      onMutated={onMutated}
      openLink={openLink}
      resumeRunId={resumeRunId}
    />
  );
}
