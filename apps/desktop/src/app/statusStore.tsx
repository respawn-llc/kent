import type { ReactNode } from "react";
import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { StatusSurface, type StatusNotice } from "../ui";
import { StatusContext, type StatusController } from "./statusContextValue";

export type StatusProviderProps = Readonly<{
  children: ReactNode;
}>;

export function StatusProvider({ children }: StatusProviderProps) {
  const [notices, setNotices] = useState<readonly StatusNotice[]>([]);
  const { t } = useTranslation();
  const push = useCallback((notice: StatusNotice) => {
    setNotices((current) => current.filter((item) => item.id !== notice.id).concat(notice));
  }, []);
  const dismiss = useCallback((id: string) => {
    setNotices((current) => current.filter((item) => item.id !== id));
  }, []);

  const controller = useMemo<StatusController>(
    () => ({
      notices,
      push,
      dismiss,
    }),
    [dismiss, notices, push],
  );

  return (
    <StatusContext.Provider value={controller}>
      <StatusSurface dismissLabel={t("app.close")} notices={notices} onDismiss={controller.dismiss}>
        {children}
      </StatusSurface>
    </StatusContext.Provider>
  );
}
