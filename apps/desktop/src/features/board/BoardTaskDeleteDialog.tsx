import { TaskDeleteConfirmationDialog } from "@/shared/task-delete";
import { useBoardTaskDeletion, type useBoardTaskDeletions } from "./BoardTaskDeletion";

export function BoardTaskDeleteDialog({
  taskID,
  deletions,
  onResult,
}: Readonly<{
  taskID: string;
  deletions: ReturnType<typeof useBoardTaskDeletions>;
  onResult(result: "cancel" | "confirm"): void;
}>) {
  const deletion = useBoardTaskDeletion(deletions, taskID);
  return (
    <TaskDeleteConfirmationDialog
      disabled={deletion.isPending}
      onClose={() => {
        onResult("cancel");
      }}
      onConfirm={() => {
        onResult("confirm");
      }}
    />
  );
}
