import type { ReactElement, ReactNode } from "react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useLocation, useNavigate } from "@tanstack/react-router";

import { ErrorState, LoadingState } from "@/ui";
import { ServerSetupGuide } from "./ServerSetupGuide";
import { useStartup } from "./useStartup";

export type StartupGateProps = Readonly<{
  children: ReactNode;
}>;

export function StartupGate({ children }: StartupGateProps): ReactElement {
  const startup = useStartup();
  const location = useLocation();
  const navigate = useNavigate();
  const { t } = useTranslation();
  const startupTitleKey = startup.kind === "error" ? startup.titleKey : "";

  useEffect(() => {
    if (startupTitleKey !== "startup.updateTitle") {
      return;
    }
    if (location.pathname === "/") {
      return;
    }
    void navigate({ to: "/", search: {}, replace: true });
  }, [location.pathname, navigate, startupTitleKey]);

  if (startup.kind === "loading") {
    return (
      <LoadingState
        body={t("startup.loadingBody")}
        chromePadding
        reveal={false}
        title={t("startup.loadingTitle")}
      />
    );
  }

  if (startup.kind === "server-missing") {
    return <ServerSetupGuide detail={startup.detail} onCheckAgain={startup.retry} />;
  }

  if (startup.kind === "error") {
    return (
      <ErrorState
        body={startup.body}
        chromePadding
        onRetry={startup.retry}
        reveal={false}
        retryLabel={t("app.retry")}
        title={t(startup.titleKey)}
      />
    );
  }

  return <>{children}</>;
}
