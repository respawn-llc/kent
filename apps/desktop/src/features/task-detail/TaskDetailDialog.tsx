import { useTranslation } from "react-i18next";

import { errorMessage } from "../../api/errors";
import { useOpenExternalLink } from "../../app/nativeHooks";
import { Dialog, ErrorState } from "../../ui";
import { TaskDetailContent } from "./TaskDetailContent";
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
      className="h-[min(860px,calc(100vh-32px))] w-[min(1080px,calc(100vw-32px))]"
      closeLabel={t("app.close")}
      onClose={onClose}
      open={open}
      title={t("task.title")}
    >
      <TaskDetailSurface enabled={open} onMutated={onMutated} resumeRunId={resumeRunId} taskId={taskId} />
    </Dialog>
  );
}

export type TaskDetailSurfaceProps = Readonly<{
  taskId: string;
  enabled: boolean;
  resumeRunId: string;
  onMutated?: (() => void) | undefined;
}>;

export function TaskDetailSurface({ taskId, enabled, resumeRunId, onMutated }: TaskDetailSurfaceProps) {
  const { t } = useTranslation();
  const detail = useTaskDetail(taskId, enabled);
  const activity = useTaskActivity(taskId, enabled);
  const openLink = useOpenExternalLink();

  if (detail.isPending) {
    return <p>{t("states.loading")}</p>;
  }
  if (detail.isError) {
    return <ErrorState body={errorMessage(detail.error)} reveal={false} title={t("states.error")} />;
  }
  return (
    <TaskDetailContent
      activity={activity}
      detail={detail.data}
      onMutated={onMutated}
      openLink={openLink}
      resumeRunId={resumeRunId}
    />
  );
}
