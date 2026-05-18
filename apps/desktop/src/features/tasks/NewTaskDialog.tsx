import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useMemo, useRef } from "react";
import { useForm, useWatch } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";

import { errorMessage } from "../../api/errors";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { useAppServices } from "../../app/useAppServices";
import { Badge, Button, Dialog, NativeDialogWindow, SelectField, TextArea, TextInput } from "../../ui";
import { cx } from "../../ui/classes";
import { useCreateTask, useWorkspaces } from "./useTaskMutations";

const newTaskSchema = z.object({
  title: z.string().trim().min(1),
  body: z.string(),
  sourceWorkspaceID: z.string().min(1),
});

type NewTaskFormValues = z.output<typeof newTaskSchema>;

export type NewTaskFallbackDialogProps = Readonly<{
  projectID: string;
  workflowID: string;
  onClose: () => void;
}>;

export function NewTaskFallbackDialog({ projectID, workflowID, onClose }: NewTaskFallbackDialogProps) {
  const { t } = useTranslation();

  return (
    <Dialog
      className="w-[min(calc(var(--content-max-width-task-create)+var(--space-4)*2),calc(100vw-32px))]"
      closeLabel={t("app.close")}
      onClose={onClose}
      open
      title={t("task.newTitle")}
    >
      <NewTaskForm
        className="mx-auto w-full max-w-[var(--content-max-width-task-create)]"
        onSubmitted={onClose}
        projectID={projectID}
        workflowID={workflowID}
      />
    </Dialog>
  );
}

export function NewTaskWindowRoute({
  projectID,
  workflowID,
}: Readonly<{
  projectID: string;
  workflowID: string;
}>) {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();

  return (
    <NativeDialogWindow contentMaxWidth="var(--content-max-width-task-create)" fitToContent={false} title={t("task.newTitle")}>
      <NewTaskForm
        className="w-full"
        onSubmitted={() => {
          void nativeBridge.window.closeCurrent();
        }}
        projectID={projectID}
        workflowID={workflowID}
      />
    </NativeDialogWindow>
  );
}

export function NewTaskForm({
  className,
  onSubmitted,
  projectID,
  workflowID,
}: Readonly<{
  className?: string;
  onSubmitted: () => void;
  projectID: string;
  workflowID: string;
}>) {
  const { t } = useTranslation();
  const connection = useConnectionSnapshot();
  const workspaces = useWorkspaces(projectID);
  const createTask = useCreateTask(projectID, workflowID);
  const defaultWorkspaceID = workspaces.data?.defaultWorkspaceID ?? "";
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
    if (!initializedOpenRef.current && defaultWorkspaceID.length > 0) {
      form.setValue("sourceWorkspaceID", defaultWorkspaceID, {
        shouldDirty: false,
        shouldTouch: false,
        shouldValidate: true,
      });
      initializedOpenRef.current = true;
    }
  }, [defaultWorkspaceID, form]);

  async function submit(values: NewTaskFormValues): Promise<void> {
    await createTask.mutateAsync({
      projectID,
      workflowID,
      title: values.title,
      body: values.body,
      sourceWorkspaceID: values.sourceWorkspaceID,
    });
    onSubmitted();
  }

  const workspaceItems = useMemo(() => workspaces.data?.workspaces ?? [], [workspaces.data?.workspaces]);
  const workspaceOptions = useMemo(
    () => workspaceItems.map((workspace) => ({ label: workspace.name, value: workspace.id })),
    [workspaceItems],
  );
  const selectedWorkspaceID = useWatch({ control: form.control, name: "sourceWorkspaceID" });
  const disabled = connection.phase !== "connected" || createTask.isPending || defaultWorkspaceID.length === 0;

  return (
    <form
      className={cx("grid gap-[var(--space-3)]", className)}
      onSubmit={(event) => void form.handleSubmit(submit)(event)}
    >
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
          name="sourceWorkspaceID"
          onValueChange={(value) => {
            form.setValue("sourceWorkspaceID", value, { shouldDirty: true, shouldTouch: true, shouldValidate: true });
          }}
          options={workspaceOptions}
          value={selectedWorkspaceID}
        />
      )}
      {createTask.error !== null ? (
        <p className="m-0 text-[var(--color-error)]">{errorMessage(createTask.error)}</p>
      ) : null}
      <Button className="mx-auto w-full max-w-[400px]" disabled={disabled} type="submit" variant="primary">
        {t("task.create")}
      </Button>
    </form>
  );
}
