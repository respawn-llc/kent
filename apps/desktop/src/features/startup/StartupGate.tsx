import type { ReactElement, ReactNode } from "react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useLocation, useNavigate } from "@tanstack/react-router";

import { ErrorState, LoadingState } from "@/ui";
import { ServerSetupGuide } from "./ServerSetupGuide";
import { useStartup } from "./useStartup";
import { ConnectionForm } from "@/shared/connection";

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
    return (
      <StartupConnection>
        <ServerSetupGuide detail={startup.detail} onCheckAgain={startup.retry} />
      </StartupConnection>
    );
  }

  if (startup.kind === "error") {
    return (
      <StartupConnection>
        <ErrorState
          body={startup.body}
          chromePadding
          onRetry={startup.retry}
          reveal={false}
          retryLabel={t("app.retry")}
          title={t(startup.titleKey)}
        />
      </StartupConnection>
    );
  }

  return <>{children}</>;
}

function StartupConnection({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <div className="flex h-full min-h-0 flex-col overflow-auto">
      {children}
      <div className="mx-auto w-full max-w-xl p-[var(--space-5)]">
        <ConnectionForm startup />
      </div>
    </div>
  );
}
