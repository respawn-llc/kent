import { useTranslation } from "react-i18next";

import { Button, NativeDialogWindow } from "../ui";
import { useAppServices } from "./useAppServices";

export function InvalidNativeDialogRoute() {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();
  return (
    <NativeDialogWindow contentMaxWidth="420px" title={t("app.invalidNativeDialogTitle")}>
      <div className="grid gap-[var(--space-3)]">
        <p className="m-0 text-sm text-[var(--color-on-island)]">
          {t("app.invalidNativeDialogBody")}
        </p>
        <Button
          className="justify-self-end"
          onClick={() => {
            void nativeBridge.window.closeCurrent();
          }}
        >
          {t("app.close")}
        </Button>
      </div>
    </NativeDialogWindow>
  );
}
