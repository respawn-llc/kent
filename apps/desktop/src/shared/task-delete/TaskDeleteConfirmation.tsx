import { useTranslation } from "react-i18next";

import { Button, compactDialogWidth, Dialog } from "@/ui";

export function TaskDeleteConfirmationDialog({
  disabled,
  onClose,
  onConfirm,
}: Readonly<{
  disabled: boolean;
  onClose: () => void;
  onConfirm: () => void;
}>) {
  const { t } = useTranslation();
  return (
    <Dialog
      closeLabel={t("app.close")}
      onClose={onClose}
      open
      title={t("board.deleteTaskTitle")}
      width={compactDialogWidth}
    >
      <div className="grid gap-[var(--space-3)]">
        <p className="m-0 text-sm text-[var(--color-on-island)]">{t("board.deleteTaskBody")}</p>
        <div className="grid grid-cols-2 gap-[var(--space-2)]">
          <Button className="w-full" disabled={disabled} onClick={onClose}>
            {t("app.cancel")}
          </Button>
          <Button className="w-full" disabled={disabled} onClick={onConfirm} variant="danger">
            {t("board.deleteTaskConfirm")}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
