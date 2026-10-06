import type { ReactNode } from "react";

import { MarkdownLinkProvider } from "@/ui";

import { AppServicesContext } from "./appServicesContextValue";
import { useOpenExternalLink } from "./nativeHooks";
import type { AppServices } from "./services";

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
  return <MarkdownLinkProvider openExternal={openExternal}>{children}</MarkdownLinkProvider>;
}
