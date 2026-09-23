import { useEffect, useState } from "react";
import { useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { errorMessage, isTaskMissingError } from "@/api";
import type {
  SidebarDestination,
  SidebarMode,
  SidebarPageNavigator,
  SidebarRootController,
  TaskDetailInitialFocus,
} from "@/app-facade";
import { useAppServices, useSidebarBackWhen } from "@/app-facade";
import { useStatusController } from "@/app-facade";
import { ProjectLabelsProvider, TaskLabelAssignmentProvider } from "@/shared/labels";
import { ErrorState, LoadingState } from "@/ui";
import { TaskDetailContent } from "./TaskDetailContent";
import type { TaskDetailDeleteDismissal } from "./taskDetailDismissal";
import type { TaskDetailSessionChatEntry } from "./taskDetailSessionChat";
import {
  createTaskDetailViewModel,
  useTaskDetailActions,
  useTaskDetailObservation,
  useTaskDetailReads,
} from "./TaskDetailViewModel";

type TaskDetailSurfaceCommonProps = Readonly<{
  taskId: string;
  enabled: boolean;
  initialFocus?: TaskDetailInitialFocus | undefined;
  onMutated?: (() => void) | undefined;
  openSessionChat?: TaskDetailSessionChatEntry | undefined;
  openSidebar?: SidebarRootController["open"] | undefined;
  retainedState?: unknown;
  sidebarDestination?: Extract<SidebarDestination, { kind: "taskDetail" }> | undefined;
  sidebarMode?: SidebarMode | undefined;
}>;

export type TaskDetailSurfaceProps = TaskDetailSurfaceCommonProps &
  (
    | Readonly<{
        navigator: SidebarPageNavigator;
        onDeleteDismiss?: undefined;
      }>
    | Readonly<{
        navigator?: undefined;
        onDeleteDismiss: TaskDetailDeleteDismissal;
      }>
  );

export function TaskDetailSurface(props: TaskDetailSurfaceProps) {
  return <TaskDetailDestination key={props.taskId} {...props} />;
}

function TaskDetailDestination({
  taskId,
  enabled,
  initialFocus,
  navigator,
  onDeleteDismiss,
  onMutated,
  openSessionChat,
  openSidebar,
  retainedState,
  sidebarDestination,
  sidebarMode,
}: TaskDetailSurfaceProps) {
  const { t } = useTranslation();
  const { push } = useStatusController();
  const services = useAppServices();
  const client = useQueryClient();
  const [model] = useState(() =>
    createTaskDetailViewModel({ services, client, taskID: taskId, enabled, retainedState, t, push }),
  );
  const { detail, attention, activity, comments } = useTaskDetailReads(model, enabled);
  const actions = useTaskDetailActions(model);
  const observation = useTaskDetailObservation(model);
  const capture = useAtomValue(model.editing.capture);
  useEffect(() => {
    if (navigator === undefined || detail.data === undefined) return;
    return navigator.registerCapture(capture);
  }, [capture, detail.data, navigator]);
  const deleteDismissal: TaskDetailDeleteDismissal =
    navigator === undefined ? onDeleteDismiss : async () => ({ kind: navigator.close() });
  const taskMissing = detail.isError && isTaskMissingError(detail.error);
  useSidebarBackWhen(taskMissing, navigator);
  if (detail.isPending) {
    return <LoadingState appearanceDelayMs={0} fullPage={false} reveal={false} title={t("states.loading")} />;
  }
  if (detail.isError) {
    if (taskMissing && navigator !== undefined) return null;
    return (
      <ErrorState
        body={errorMessage(detail.error)}
        reveal={false}
        title={t("states.error")}
        onRetry={() => {
          detail.refetch();
        }}
        retryLabel={t("app.retry")}
      />
    );
  }
  const failure = [
    {
      error: attention.error,
      retry: () => {
        attention.refetch();
      },
    },
    observation,
  ].find(({ error }) => error !== null);
  if (failure !== undefined)
    return (
      <ErrorState
        body={errorMessage(failure.error)}
        title={t("states.error")}
        onRetry={failure.retry}
        retryLabel={t("app.retry")}
      />
    );
  return (
    <ProjectLabelsProvider
      onBackgroundError={model.reportLabelError}
      projectID={detail.data.projectID}
      subscribeToProject={false}
    >
      <TaskLabelAssignmentProvider key={detail.data.id} taskID={detail.data.id}>
        <TaskDetailContent
          key={detail.data.id}
          activity={activity}
          attention={attention}
          comments={comments}
          detail={detail.data}
          model={model}
          actions={actions}
          initialFocus={initialFocus}
          navigator={navigator}
          onDeleteDismiss={deleteDismissal}
          onMutated={onMutated}
          openSessionChat={openSessionChat}
          openSidebar={openSidebar}
          sidebarDestination={sidebarDestination}
          sidebarMode={sidebarMode}
        />
      </TaskLabelAssignmentProvider>
    </ProjectLabelsProvider>
  );
}
