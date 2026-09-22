import { createContext, useContext, useState, type ReactNode } from "react";
import { Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import { TaskDeleteConfirmationDialog } from "@/shared/task-delete";
import { Button } from "@/ui";

type TaskDeleteController = Readonly<{
  running: boolean;
  open(): void;
}>;

const TaskDeleteContext = createContext<TaskDeleteController | null>(null);

export function TaskDeleteProvider({
  children,
  pending,
  onDelete,
}: Readonly<{
  children: ReactNode;
  pending: boolean;
  onDelete(): void;
}>) {
  const [open, setOpen] = useState(false);

  return (
    <TaskDeleteContext.Provider
      value={{
        running: pending,
        open() {
          setOpen(true);
        },
      }}
    >
      {children}
      {open ? (
        <TaskDeleteConfirmationDialog
          disabled={pending}
          onClose={() => {
            setOpen(false);
          }}
          onConfirm={() => {
            setOpen(false);
            onDelete();
          }}
        />
      ) : null}
    </TaskDeleteContext.Provider>
  );
}

export function TaskDeleteButton({
  active,
  disabled,
}: Readonly<{
  active: boolean;
  disabled: boolean;
}>) {
  const { t } = useTranslation();
  const controller = useContext(TaskDeleteContext);
  if (controller === null) {
    throw new Error("Task Delete button requires a Task Delete provider");
  }
  return (
    <Button
      aria-hidden={!active}
      aria-label={t("board.deleteTask")}
      className={`absolute inset-0 transition-opacity motion-reduce:transition-none ${
        active ? "pointer-events-auto opacity-100" : "pointer-events-none opacity-0 disabled:opacity-0!"
      }`}
      data-testid="task-detail-delete"
      disabled={disabled || controller.running || !active}
      onClick={controller.open}
      size="icon"
      tabIndex={active ? undefined : -1}
      title={active ? t("board.deleteTask") : undefined}
      variant="danger"
    >
      <Trash2 aria-hidden="true" size={16} strokeWidth={1.75} />
    </Button>
  );
}
