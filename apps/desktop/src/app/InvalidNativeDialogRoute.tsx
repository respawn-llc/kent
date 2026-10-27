import { useTranslation } from "react-i18next";

import { useAppServices } from "@/app-facade";
import { Button } from "@/ui";

export function InvalidNativeDialogRoute() {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();
  return (
    <main className="grid h-screen place-content-center gap-[var(--space-3)] p-[var(--space-4)]">
      <h1>{t("app.invalidNativeDialogTitle")}</h1>
      <div className="grid gap-[var(--space-3)]">
        <p className="m-0 text-sm text-[var(--color-on-island)]">{t("app.invalidNativeDialogBody")}</p>
        <Button
          className="justify-self-end"
          onClick={() => {
            void nativeBridge.window.closeCurrent();
          }}
        >
          {t("app.close")}
        </Button>
      </div>
    </main>
  );
}
