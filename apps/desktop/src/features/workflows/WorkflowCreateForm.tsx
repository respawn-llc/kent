import { useState, type SyntheticEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import {
  useAppServices,
  useQueryAction,
  useStatusController,
  useTextFieldSubmitShortcut,
} from "@/app-facade";
import { Button, TextArea, TextInput } from "@/ui";
import { createWorkflowCreateModel, type WorkflowCreateResult } from "./WorkflowCreateModel";

export function WorkflowCreateForm({
  onCreated,
  onProjectMissing,
  projectID,
}: Readonly<{
  onCreated: (result: WorkflowCreateResult) => void;
  onProjectMissing?: (() => void) | undefined;
  projectID?: string | undefined;
}>) {
  const { t } = useTranslation();
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  const { push } = useStatusController();
  const [model] = useState(() => createWorkflowCreateModel({ api, client: queryClient, projectID, push, t }));
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const create = useQueryAction(model);
  const canSubmit = name.trim().length > 0 && !create.isPending;
  const formShortcut = useTextFieldSubmitShortcut({
    available: canSubmit,
    kind: "form",
  });

  function submit(event: SyntheticEvent<HTMLFormElement>): void {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    create.submit({ name: name.trim(), description: description.trim(), onCreated, onProjectMissing });
  }

  return (
    <form className="grid gap-[var(--space-4)]" onKeyDown={formShortcut} onSubmit={submit}>
      <TextInput
        autoFocus
        label={t("workflowLibrary.name")}
        onChange={(event) => {
          setName(event.target.value);
        }}
        placeholder={t("workflowLibrary.namePlaceholder")}
        value={name}
      />
      <TextArea
        label={t("workflowLibrary.description")}
        onChange={(event) => {
          setDescription(event.target.value);
        }}
        placeholder={t("workflowLibrary.descriptionPlaceholder")}
        value={description}
      />
      <div className="flex justify-end gap-[var(--space-2)]">
        <Button disabled={!canSubmit} type="submit" variant="primary">
          {create.isPending ? t("workflowLibrary.creating") : t("workflowLibrary.createWorkflow")}
        </Button>
      </div>
    </form>
  );
}
