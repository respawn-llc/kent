import { useAppNavigation } from "../../app/navigation";
import { TaskDetailDialog } from "./TaskDetailDialog";

export type StandaloneTaskRouteProps = Readonly<{
  taskId: string;
}>;

export function StandaloneTaskRoute({ taskId }: StandaloneTaskRouteProps) {
  const navigation = useAppNavigation();
  return <TaskDetailDialog onClose={navigation.openHome} open resumeRunId="" taskId={taskId} />;
}
