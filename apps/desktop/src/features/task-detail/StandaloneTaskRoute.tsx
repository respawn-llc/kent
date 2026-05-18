import { useTranslation } from "react-i18next";

import { useAppNavigation } from "../../app/navigation";
import { useAppServices } from "../../app/useAppServices";
import { Button, NativeDialogWindow } from "../../ui";
import { TaskDetailSurface } from "./TaskDetailDialog";
import { useNativeTaskDetailTarget } from "./taskDetailNativeHooks";

export type StandaloneTaskRouteProps = Readonly<{
  taskId: string;
}>;

export type TaskDetailWindowRouteProps = Readonly<{
  taskId: string;
  resumeRunId: string;
}>;

export function StandaloneTaskRoute({ taskId }: StandaloneTaskRouteProps) {
  const { t } = useTranslation();
  const navigation = useAppNavigation();
  return (
    <section className="island-glass grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-[var(--space-4)] rounded-[var(--radius-xl)] p-[var(--space-4)]">
      <header className="flex items-center justify-between gap-[var(--space-3)]">
        <h1 className="m-0 text-[1.25rem]">{t("task.title")}</h1>
        <Button onClick={() => void navigation.openHome()} variant="ghost">
          {t("app.backHome")}
        </Button>
      </header>
      <TaskDetailSurface enabled resumeRunId="" taskId={taskId} />
    </section>
  );
}

export function TaskDetailWindowRoute({ resumeRunId, taskId }: TaskDetailWindowRouteProps) {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();
  const target = useNativeTaskDetailTarget(taskId, resumeRunId);

  return (
    <NativeDialogWindow fitToContent={false} title={t("task.title")}>
      <div className="h-full min-h-0 w-full min-w-0">
        <TaskDetailSurface
          enabled
          onMutated={() => {
            void nativeBridge.taskDetail.notifyChanged({ taskId: target.taskId });
          }}
          resumeRunId={target.resumeRunId}
          taskId={target.taskId}
        />
      </div>
    </NativeDialogWindow>
  );
}
