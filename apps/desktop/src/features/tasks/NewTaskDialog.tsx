import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";

import type { WorkflowBoard } from "../../api";
import { errorMessage } from "../../api/errors";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { Badge, Button, Dialog, SelectField, TextArea, TextInput } from "../../ui";
import { useCreateTask, useWorkspaces } from "./useTaskMutations";

const newTaskSchema = z.object({
  title: z.string().trim().min(1),
  body: z.string(),
  sourceWorkspaceID: z.string(),
});

type NewTaskFormValues = z.output<typeof newTaskSchema>;

export type NewTaskDialogProps = Readonly<{
  board: WorkflowBoard;
  open: boolean;
  onClose: () => void;
}>;

export function NewTaskDialog({ board, open, onClose }: NewTaskDialogProps) {
  const { t } = useTranslation();
  const connection = useConnectionSnapshot();
  const workspaces = useWorkspaces(board.projectID);
  const createTask = useCreateTask(board.projectID, board.selectedWorkflow.id);
  const defaultWorkspaceID = workspaces.data?.defaultWorkspaceID ?? "";
  const form = useForm<NewTaskFormValues>({
    resolver: zodResolver(newTaskSchema),
    defaultValues: {
      title: "",
      body: "",
      sourceWorkspaceID: defaultWorkspaceID,
    },
  });

  useEffect(() => {
    if (open && defaultWorkspaceID.length > 0) {
      form.reset({ title: "", body: "", sourceWorkspaceID: defaultWorkspaceID });
    }
  }, [defaultWorkspaceID, form, open]);

  async function submit(values: NewTaskFormValues): Promise<void> {
    await createTask.mutateAsync({
      projectID: board.projectID,
      workflowID: board.selectedWorkflow.id,
      title: values.title,
      body: values.body,
      sourceWorkspaceID: values.sourceWorkspaceID,
    });
    onClose();
  }

  const workspaceItems = workspaces.data?.workspaces ?? [];
  const disabled = connection.phase !== "connected" || createTask.isPending;

  return (
    <Dialog closeLabel={t("app.close")} onClose={onClose} open={open} title={t("task.newTitle")}>
      <form className="stack" onSubmit={(event) => void form.handleSubmit(submit)(event)}>
        <TextInput error={form.formState.errors.title !== undefined ? t("form.required") : undefined} label={t("task.name")} {...form.register("title")} />
        <TextArea label={t("task.body")} placeholder={t("task.bodyPlaceholder")} rows={6} {...form.register("body")} />
        {workspaceItems.length === 1 ? (
          <Badge tone="info">{workspaceItems[0]?.name ?? t("task.sourceWorkspace")}</Badge>
        ) : (
          <SelectField disabled={workspaceItems.length <= 1} label={t("task.sourceWorkspace")} {...form.register("sourceWorkspaceID")}>
            {workspaceItems.map((workspace) => (
              <option key={workspace.id} value={workspace.id}>
                {workspace.name}
              </option>
            ))}
          </SelectField>
        )}
        {createTask.error !== null ? <p className="form-error">{errorMessage(createTask.error)}</p> : null}
        <Button disabled={disabled} type="submit" variant="primary">
          {t("task.create")}
        </Button>
      </form>
    </Dialog>
  );
}
