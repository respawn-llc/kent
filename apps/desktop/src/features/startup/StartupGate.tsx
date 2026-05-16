import type { ReactElement, ReactNode } from "react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";

import { useStatusController } from "../../app/useStatusController";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { ErrorState, LoadingState } from "../../ui";
import { useStartup } from "./useStartup";

export type StartupGateProps = Readonly<{
  children: ReactNode;
}>;

export function StartupGate({ children }: StartupGateProps): ReactElement {
  const startup = useStartup();
  const connection = useConnectionSnapshot();
  const { dismiss, push } = useStatusController();
  const { t } = useTranslation();

  useEffect(() => {
    if (connection.phase !== "disconnected") {
      dismiss("connection-lost");
      return;
    }
    push({
      id: "connection-lost",
      tone: "warning",
      title: t("app.reconnecting"),
      body: t("app.disconnected"),
    });
  }, [connection.phase, dismiss, push, t]);

  if (startup.kind === "loading") {
    return <LoadingState body={t("startup.loadingBody")} title={t("startup.loadingTitle")} />;
  }

  if (startup.kind === "error") {
    return (
      <ErrorState body={startup.body} onRetry={startup.retry} retryLabel={t("app.retry")} title={t(startup.titleKey)}>
        {startup.titleKey === "startup.unreachableTitle" ? <p>{t("startup.unreachableBody")}</p> : null}
      </ErrorState>
    );
  }

  return <>{children}</>;
}
