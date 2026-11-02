import { useState } from "react";
import { flushSync } from "react-dom";
import { useTranslation } from "react-i18next";
import { useAtomValue } from "@effect/atom-react";
import { Trash2 } from "lucide-react";

import { Button, compactDialogWidth, Dialog } from "@/ui";
import { useProjectEditActions, type ProjectEditViewModel } from "./ProjectEditViewModel";

type ProjectDeleteResult = "cancel" | "confirm";

export function ProjectDeleteButton({
  model,
  openHome,
}: Readonly<{ model: ProjectEditViewModel; openHome(): Promise<void> }>) {
  const { t } = useTranslation();
  const state = useAtomValue(model.state);
  const disabled = state.pending;
  const { deleteProject } = useProjectEditActions(model);
  const [confirming, setConfirming] = useState(false);
  return (
    <>
      {confirming ? (
        <ProjectDeleteConfirmation
          disabled={disabled}
          onResult={(result) => {
            flushSync(() => {
              setConfirming(false);
            });
            if (result === "confirm") deleteProject({ openHome });
          }}
        />
      ) : null}
      <Button
        aria-label={t("projectEdit.deleteProject")}
        className="justify-self-end"
        disabled={disabled}
        loading={state.deletePending}
        onClick={() => {
          setConfirming(true);
        }}
        size="icon"
        title={t("projectEdit.deleteProject")}
        variant="danger"
      >
        <Trash2 aria-hidden="true" className="block" size={18} strokeWidth={1.5} />
      </Button>
    </>
  );
}

function ProjectDeleteConfirmation({
  disabled,
  onResult,
}: Readonly<{ disabled: boolean; onResult(result: ProjectDeleteResult): void }>) {
  const { t } = useTranslation();
  return (
    <Dialog
      closeLabel={t("app.close")}
      onClose={() => {
        onResult("cancel");
      }}
      open
      title={t("projectEdit.deleteTitle")}
      width={compactDialogWidth}
    >
      <div className="grid gap-[var(--space-3)]">
        <p className="m-0 text-sm text-[var(--color-on-island)]">{t("projectEdit.deleteBody")}</p>
        <div className="grid grid-cols-2 gap-[var(--space-2)]">
          <Button
            className="w-full"
            disabled={disabled}
            onClick={() => {
              onResult("cancel");
            }}
          >
            {t("app.cancel")}
          </Button>
          <Button
            className="w-full"
            disabled={disabled}
            onClick={() => {
              onResult("confirm");
            }}
            variant="danger"
          >
            {t("projectEdit.deleteConfirm")}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
