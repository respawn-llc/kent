import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useRef } from "react";
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
  sourceWorkspaceID: z.string().min(1),
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
  const workspaceItems = workspaces.data?.workspaces ?? [];
  const fallbackWorkspaceID = workspaceItems[0]?.id ?? "";
  const initializedOpenRef = useRef(false);
  const form = useForm<NewTaskFormValues>({
    resolver: zodResolver(newTaskSchema),
    defaultValues: {
      title: "",
      body: "",
      sourceWorkspaceID: defaultWorkspaceID,
    },
  });

  useEffect(() => {
    if (!open) {
      initializedOpenRef.current = false;
      return;
    }
    const sourceWorkspaceID = defaultWorkspaceID.length > 0 ? defaultWorkspaceID : fallbackWorkspaceID;
    if (!initializedOpenRef.current && sourceWorkspaceID.length > 0) {
      form.reset({ title: "", body: "", sourceWorkspaceID });
      initializedOpenRef.current = true;
    }
  }, [defaultWorkspaceID, fallbackWorkspaceID, form, open]);

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

  const disabled = connection.phase !== "connected" || createTask.isPending;

  return (
    <Dialog closeLabel={t("app.close")} onClose={onClose} open={open} title={t("task.newTitle")}>
      <form className="grid gap-[var(--space-3)]" onSubmit={(event) => void form.handleSubmit(submit)(event)}>
        <TextInput
          error={form.formState.errors.title !== undefined ? t("form.required") : undefined}
          label={t("task.name")}
          {...form.register("title")}
        />
        <TextArea
          label={t("task.body")}
          placeholder={t("task.bodyPlaceholder")}
          rows={6}
          {...form.register("body")}
        />
        {workspaceItems.length === 1 ? (
          <Badge tone="info">{workspaceItems[0]?.name ?? t("task.sourceWorkspace")}</Badge>
        ) : (
          <SelectField
            disabled={workspaceItems.length <= 1}
            label={t("task.sourceWorkspace")}
            {...form.register("sourceWorkspaceID")}
          >
            {workspaceItems.map((workspace) => (
              <option key={workspace.id} value={workspace.id}>
                {workspace.name}
              </option>
            ))}
          </SelectField>
        )}
        {createTask.error !== null ? (
          <p className="m-0 text-[var(--color-error)]">{errorMessage(createTask.error)}</p>
        ) : null}
        <Button disabled={disabled} type="submit" variant="primary">
          {t("task.create")}
        </Button>
      </form>
    </Dialog>
  );
}
