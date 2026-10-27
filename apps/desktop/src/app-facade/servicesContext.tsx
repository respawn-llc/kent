import { useCallback, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { MarkdownLinkProvider } from "@/ui/markdownLinks";
import { showStatusToast } from "@/ui";
import { errorMessage } from "@/api";

import { AppServicesContext } from "./appServicesContextValue";
import { useOpenExternalLink } from "./nativeHooks";
import type { AppServices } from "./services";
import { useAppServices } from "./useAppServices";

export type AppServicesProviderProps = Readonly<{
  services: AppServices;
  children: ReactNode;
}>;

export function AppServicesProvider({ services, children }: AppServicesProviderProps) {
  return (
    <AppServicesContext.Provider value={services}>
      <AppServicesMarkdownLinks>{children}</AppServicesMarkdownLinks>
    </AppServicesContext.Provider>
  );
}

function AppServicesMarkdownLinks({ children }: Readonly<{ children: ReactNode }>) {
  const openExternal = useOpenExternalLink();
  const { nativeBridge } = useAppServices();
  const { t } = useTranslation();
  const openFile = useCallback(
    (path: string) => {
      void nativeBridge.files.openDefault({ path }).catch((cause: unknown) => {
        showStatusToast({
          id: "markdown-file-open-failed",
          title: t("app.fileOpenFailed"),
          body: errorMessage(cause),
          tone: "danger",
        });
      });
    },
    [nativeBridge, t],
  );
  return (
    <MarkdownLinkProvider
      openExternal={openExternal}
      openFile={nativeBridge.capabilities.files.open ? openFile : undefined}
    >
      {children}
    </MarkdownLinkProvider>
  );
}
