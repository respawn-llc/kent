import { TaskDeleteConfirmationDialog } from "@/shared/task-delete";
import { useBoardDeleteConfirmation } from "./BoardTaskDeletion";

export function BoardTaskDeleteDialog(input: Parameters<typeof useBoardDeleteConfirmation>[0]) {
  const deletion = useBoardDeleteConfirmation(input);
  return (
    <TaskDeleteConfirmationDialog
      disabled={deletion.isPending}
      onClose={input.onClose}
      onConfirm={deletion.confirm}
    />
  );
}
