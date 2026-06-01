import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { Trash2 } from "lucide-react";

import { errorMessage } from "../../api/errors";
import { useAppNavigation } from "../../app/navigation";
import { clearLastProjectRoute } from "../../app/projectRoutePersistence";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { useSidebar } from "../../app/sidebarContext";
import { useStatusController } from "../../app/useStatusController";
import { Button, Dialog } from "../../ui";
import { useProjectDelete } from "./useProjectEditData";

export function ProjectDeleteButton({ projectID }: Readonly<{ projectID: string }>) {
  const { t } = useTranslation();
  const connection = useConnectionSnapshot();
  const { closeSidebar } = useSidebar();
  const navigation = useAppNavigation();
  const { push } = useStatusController();
  const mutation = useProjectDelete(projectID);
  const [open, setOpen] = useState(false);
  const disabled = connection.phase !== "connected" || mutation.isPending;

  const confirmDelete = useCallback(async () => {
    try {
      const response = await mutation.mutateAsync();
      if (!response.deleted) {
        push({
          id: "project-delete-blocked",
          tone: "danger",
          title: t("projectEdit.deleteBlocked"),
          body: response.blockers.map((blocker) => blocker.message).join("\n"),
        });
        return;
      }
      setOpen(false);
      clearLastProjectRoute(projectID);
      closeSidebar("closed");
      await navigation.openHome();
      push({
        id: "project-delete-deleted",
        tone: "success",
        title: t("projectEdit.deleteTitle"),
        body: t("projectEdit.deleteDeleted"),
      });
    } catch (error) {
      push({
        id: "project-delete-error",
        tone: "danger",
        title: t("projectEdit.deleteTitle"),
        body: errorMessage(error),
      });
    }
  }, [closeSidebar, mutation, navigation, projectID, push, t]);

  return (
    <>
      <Button
        aria-label={t("projectEdit.deleteProject")}
        className="justify-self-end"
        disabled={disabled}
        onClick={() => {
          setOpen(true);
        }}
        size="icon"
        title={t("projectEdit.deleteProject")}
        variant="danger"
      >
        <Trash2 aria-hidden="true" className="block" size={18} strokeWidth={1.5} />
      </Button>
      <Dialog
        closeLabel={t("app.close")}
        onClose={() => {
          setOpen(false);
        }}
        open={open}
        title={t("projectEdit.deleteTitle")}
      >
        <div className="grid gap-[var(--space-3)]">
          <p className="m-0 text-sm text-[var(--color-on-island)]">{t("projectEdit.deleteBody")}</p>
          <div className="grid grid-cols-2 gap-[var(--space-2)]">
            <Button
              className="w-full"
              disabled={mutation.isPending}
              onClick={() => {
                setOpen(false);
              }}
            >
              {t("app.cancel")}
            </Button>
            <Button
              className="w-full"
              disabled={mutation.isPending}
              onClick={() => {
                void confirmDelete();
              }}
              variant="danger"
            >
              {t("projectEdit.deleteConfirm")}
            </Button>
          </div>
        </div>
      </Dialog>
    </>
  );
}
